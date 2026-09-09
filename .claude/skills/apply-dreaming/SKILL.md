---
name: apply-dreaming
description: "Read the latest ragivka dreaming report and apply
  high-confidence findings. Routes spec/architecture/invariant/code
  changes through /backlog -> issue -> /ship. Direct branch+PR only
  for tooling scripts that don't touch requirements or the Critical
  Invariants. Annotates report with [applied YYYY-MM-DD] markers."
user-invocable: true
argument-hint: "[week|latest]"
---

# /apply-dreaming (ragivka)

Walks each dreaming-report finding interactively and leaves an audit
trail (`[applied YYYY-MM-DD]` / `[planned …]` / `[skipped …]` markers
appended to the report).

## When to invoke

- Monday morning after the Sunday cron-run produced a fresh report.
- Or any time after a manual `.claude/dreaming/dreaming.sh` run.
- Or `/apply-dreaming` (with optional `latest` or `2026-W##` argument).

## Inputs

- Optional argument: `latest` (default) or `YYYY-W##`.
- Project root: `~/wrk/projects/ragivka/ragivka/`.

## Steps

### 1. Locate report

```bash
WEEK="${1:-latest}"
DIR=".claude/dreaming/reports"
if [[ "$WEEK" == "latest" ]]; then
  REPORT=$(find "$DIR" -maxdepth 1 -type f -name '[0-9][0-9][0-9][0-9]-W*.md' \
           -printf '%T@ %p\n' 2>/dev/null \
           | sort -rn | head -1 | cut -d' ' -f2-)
else
  REPORT="$DIR/$WEEK.md"
fi
```

### 2. Parse into structured items

Read REPORT. For each numbered sub-item under a section, extract:

- `id`, `title`, `confidence` (high/medium/low)
- `evidence` — file:line / requirement id / commit sha
- `suggestion`
- `category` — infer from the suggestion:
  - "update `docs/requirements.md`" / "update `docs/architecture.md`" → `update-spec`
  - **any invariant finding** (tenant isolation NFR-16, transaction
    boundaries NFR-7, write-tool idempotency NFR-4/NFR-15) → `invariant-fix`
    — always highest-priority, never downgrade its routing even if
    reported at medium/low confidence
  - "code change / fix drift / implement" → `code-change`
  - "resume or delete plan" → `stale-plan`
  - "new `CLAUDE.md` convention / merge skills" → `update-tooling`
  - else → `other`

**Confidence inheritance.** If a sub-item has no explicit `confidence`
field, inherit it from the enclosing section. Default `medium` only
when neither declares one.

**Idempotency — skip already-marked items.** If the next non-blank
line after an item starts with `> [applied …]`, `> [planned …]`,
`> [skipped …]`, or `> [manual-review-required …]`, skip it silently.
The report is appended to (never rewritten) on each pass. Print a
summary at the start: `2026-W##: M new items (N already-processed
skipped)`.

### 3. Show TL;DR + counts

```
2026-W##: N items (X high, Y medium, Z low)
TL;DR: ...
Process all? [y/select/skip-low/abort]
```

### 4. Triage walk

Iterate `high → medium → low`, but **surface any `invariant-fix` item
first regardless of its stated confidence** — tenant isolation /
transaction boundary / idempotency violations are security- and
correctness-adjacent, worth a human's eyes even at "low" confidence.

```
[H 1/N] §<id>  <title>
  Evidence: <file:line / requirement-id / commit>
  Suggestion: <suggestion>

  [a]pply / [s]kip / [v]erify-first / [e]vidence / [q]uit
```

For `low` (non-invariant): skip silently unless the user opted in at
step 3.

### 5. Apply per category

**Routing rule.** Two paths:

- **Plan-and-gate path** for anything touching the spec or code
  behavior: `update-spec`, `invariant-fix`, `code-change`,
  `update-tooling` (new `CLAUDE.md` convention). All go through
  `/backlog` (plan → GitHub issue) → `/ship`. `docs/requirements.md`
  and `docs/architecture.md` are the controlling artifacts — never
  edit them directly from a dreaming finding, even high-confidence.
- **Direct branch+PR path** only for `stale-plan` cleanup and
  `.claude/dreaming/*.sh` script fixes that don't touch requirements
  or the invariants.

#### `update-spec` / `invariant-fix` / `code-change` / `update-tooling` — plan-and-gate

1. Draft a plan referencing the dreaming finding
   (`.claude/plans/<priority>-dreaming-W##-<slug>.md`), following
   `/backlog`'s own conventions.
2. Plan body: cite report §<id>, evidence (file:line / requirement id
   / commit sha), suggested change, files to touch, acceptance
   criteria. For `invariant-fix`, explicitly restate which of the
   three Critical Invariants is at risk and why.
3. Tell the user: "Created plan `<priority>-dreaming-W##-<slug>`. Run
   `/backlog <slug>` to gate it and create the GitHub issue. Then
   `/ship` for implementation."
4. Don't implement directly — `/backlog` → issue → `/ship` is the only
   path.

#### `stale-plan` — direct cleanup

1. Read the plan file's content and mtime.
2. If it already has a matching GitHub issue (`gh issue list --search
   <slug>`), delete the plan file — it's done its job.
3. If it has no issue and looks abandoned, ask the user: resume via
   `/backlog <slug>` or delete.

#### `update-tooling` (dreaming script only, not the prompt) — branch+PR

For fixes to `dreaming.sh` itself (not `dreaming-prompt.md`, which
shapes what the pass looks for — route that through plan-and-gate):

1. `git switch -c fix-dreaming-w##-<slug>` off `main`.
2. Edit the script; smoke-test with `bash -n .claude/dreaming/dreaming.sh`.
3. Commit, push, open a PR, merge per the project's normal PR
   convention.

#### `other` — manual review

Print suggestion + evidence. Don't apply. Annotate
`[manual-review-required 2026-MM-DD]`.

### 6. Annotate report

After each applied item, append (never rewrite the original):

```markdown
> [applied 2026-MM-DD: <action>; commit <sha>; PR <num>]
```

For created plans:
```markdown
> [planned 2026-MM-DD: .claude/plans/<file>; awaiting /backlog]
```

For skipped:
```markdown
> [skipped 2026-MM-DD: <reason>]
```

### 7. Final summary

```
Applied: N (direct plan cleanup / tooling PRs)
Plans created: M (awaiting /backlog → /ship)
Manual review: K
Skipped: P

PRs opened: <list>
Plans pending: <list>

Next steps:
1. Run /backlog on each plan (invariant-fix plans first).
2. Run /ship on approved plans — /fix-review runs automatically as part of /ship.
```

## Constraints (CRITICAL)

- **NEVER commit or push directly to `main`.**
- **NEVER edit `docs/requirements.md` or `docs/architecture.md`
  directly** — always plan-and-gate through `/backlog`, even for a
  high-confidence finding.
- **NEVER downgrade an `invariant-fix` item's priority** based on its
  stated confidence — surface it first regardless.
- **NEVER auto-apply low confidence** (non-invariant) without explicit request.
- **ALWAYS cite report-section** (`§<id>`) in commit messages and PR bodies.
- **One PR per category-batch** — don't mix a `dreaming.sh` fix with a
  spec/invariant change in the same PR.
- **Confirm before destructive ops** even at high confidence.

## Anti-patterns

- ❌ Edit `docs/requirements.md`/`docs/architecture.md` directly.
- ❌ Commit or push to `main` directly, tooling included.
- ❌ Treat an `invariant-fix` finding as routine — it needs the same
  scrutiny as a security report, not a shrug because the model's
  confidence label said "low."
- ❌ Modify the report's original suggestions (annotate only).
- ❌ Apply a finding without verifying it against current code first.

## Companion skills

- `/backlog` — plan → GitHub issue.
- `/ship` — issue → implement → `/fix-review` → merge → close.
- `/fix-review` — parallel multi-model review + Claude Arbiter (part of `/ship`).
- `/review-deps`, `/find-bugs`, `/improve` — complementary read-only checks.

## See also

- `.claude/dreaming/dreaming-prompt.md` — what the dreaming pass looks for.
- `.claude/dreaming/dreaming.sh` — how the pass is run (systemd timer).
- `CLAUDE.md` "Critical invariants" section — target of `invariant-fix` findings.
