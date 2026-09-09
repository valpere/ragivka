You are doing a **dreaming pass** for the **ragivka** project — async,
scheduled curation of project context. This is sleep-time
consolidation: review what accumulated since last pass, identify
patterns, suggest curation. Read-only — output a report, don't modify
anything.

## Project context

- **ragivka** — multi-tenant RAG + workflow orchestration framework, Go,
  statically compiled. Seven-layer architecture (Interfaces → App API
  → Orchestrator → AI/RAG/Tool layers → Data layer); two entrypoints
  (`cmd/server`, `cmd/worker`).
- Requirements are tagged inline as `// NFR-N` / `// FR-N` comments,
  sourced from `docs/requirements.md`.
- Workflow: `/backlog` (plan → issue) → `/ship` (issue → implement →
  `/fix-review` → merge → close). Branches off `main`; PR-reviewed.

## Targets

| Path | What to look for |
|------|-------------------|
| `docs/requirements.md` vs `// NFR-*`/`// FR-*` comments in code | A requirement with no matching tagged implementation, or a tag in code with no matching requirement entry |
| **Tenant isolation (NFR-16)** | Any DB query in `pkg/*/` that doesn't route through `tenant.MustGetTenantID`/`tenant.GetTenantID` — grep for direct `pgxpool`/SQL calls not preceded by a tenant-context read in the same function |
| **Transaction boundaries (NFR-7)** | A River job handler or repository function that holds a DB transaction open across an external API call (Ollama, S3, Telegram) |
| **Write Tool idempotency (NFR-4/NFR-15)** | A Write-kind tool in `pkg/tools/` that mutates external state without an idempotency-key check + `AUDIT_LOG` write |
| `docs/architecture.md` | Stale ER diagram / FSM description vs. current `migrations/*.sql` and `pkg/runtime/fsm.go` |
| `.claude/plans/` | Plan files older than 14 days that never became a GitHub issue via `/backlog` |
| `.claude/skills/` | This project has a large skill roster (`backlog`, `debug`, `doubt-driven-development`, `find-bugs`, `fix-review`, `housekeeping`, `improve`, `onboard`, `review-deps`, `revival`, `self-learn`, `ship`) — flag any two whose descriptions now read as overlapping territory |
| Recent PR review comments | Recurring `/fix-review` themes across merged PRs, especially anything touching the Critical Invariants section |
| `git log --since="30 days ago"` | Commits touching `pkg/tenant/`, `pkg/tools/`, or River job handlers without a corresponding `docs/requirements.md`/`docs/architecture.md` update |

## What to find

### 1. Requirement/code tag drift

Sample `// NFR-*`/`// FR-*` tags in recently changed files
(`git log --since="30 days ago" -p -- 'pkg/**/*.go' 'cmd/**/*.go'`)
against `docs/requirements.md`. Flag orphaned tags (code references a
requirement id that doesn't exist in the doc) and stale doc entries
(requirement documented, no code implements or references it).

### 2. Critical-invariant violations

For each of the three invariants (tenant isolation, transaction
boundaries, write-tool idempotency), search recent commits for code
that plausibly violates the documented pattern. Cite the specific
function/file:line. Don't flag test helpers or migration scripts —
these invariants apply to production request-handling paths.

### 3. Architecture drift

Spot-check `docs/architecture.md`'s ER diagram and FSM description
against `migrations/*.sql` (schema) and `pkg/runtime/fsm.go` (actual
states/transitions). Flag any table/column/state the doc describes
that the code doesn't have, or vice versa.

### 4. Stale plans

`.claude/plans/*.md` older than 14 days that never became a GitHub
issue via `/backlog`. Flag for cleanup or resumption.

### 5. Skill roster overlap

Read `.claude/skills/*/SKILL.md` frontmatter descriptions. This
project has more skills than most (12+) — check that
`debug`/`doubt-driven-development`/`find-bugs`/`improve`/`review-deps`
still carve out genuinely distinct territory rather than five ways to
ask "is this code right."

### 6. Recurring `/fix-review` themes

`gh pr list --state merged --limit 20` + `gh pr view N --json comments`.
A theme repeating 3+ times — especially anything invariant-related —
is a candidate for a new `CLAUDE.md` convention or a `self-learn`
pattern.

## Report format

```markdown
# ragivka dreaming — <ISO week>

## TL;DR
<3-5 bullet summary>

## 1. Requirement/code tag drift
### a) <finding>
- Confidence: high|medium|low
- Evidence: <file:line, requirement id, commit sha>
- Suggest: <action>

## 2. Critical-invariant violations
...

## 3. Architecture drift
...

## 4. Stale plans
...

## 5. Skill roster overlap
...

## 6. Recurring /fix-review themes
...

## 7. Open questions
<what you couldn't verify from a read-only pass>
```

Confidence levels: **high** = directly verified against a specific
file/commit; **medium** = pattern observed but not exhaustively
checked; **low** = a hunch worth someone's attention, not a confirmed
finding. Don't fabricate evidence — say "couldn't verify" rather than
guess. Invariant violations in particular need a real code citation,
not a general impression — a false accusation here (tenant isolation,
transaction boundaries) wastes review time on a security-adjacent claim.
