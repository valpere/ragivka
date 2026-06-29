---
name: backlog
description: "Plan a task before touching code — read codebase, produce plan, get tech-lead approval, create issue, delete plan file. Usage: /backlog [task description | issue number]"
---

# Skill: /backlog
# Plan-First Development

Prevent wasted effort by aligning on approach before writing code.
For trivial changes (one-line fix, typo) skip this and just do it.
For anything touching more than one file or requiring design decisions — plan first.

The plan file is temporary. It is deleted once a task is created in the issue tracker.
The issue is the canonical record.

---

## Step 0 — No argument: show queued plans

List draft plan files from `.claude/plans/` (files without an issue yet), sorted by
priority prefix, excluding `README.md`.

```
Queued drafts:

  1. [p2] 2-health-endpoints.md — Add /health/live and /health/ready
  2. [p3] 3-chairman-prompt.md — Pass consensus score to chairman prompt

Or type a new task description:
```

Wait for selection, then proceed.

---

## Step 1 — Understand the task

**If argument is a number:** check `.claude/plans/` for a matching prefix, or fetch
an existing issue with `gh issue view <n>`.

**If argument is a description:** check `.claude/plans/` for an existing matching plan.
If found, use it. If not, create a new one.

Read `CLAUDE.md` and relevant docs.

---

## Step 2 — Read affected files

Identify every file that will change. Read them — do not guess.

---

## Step 3 — Determine metadata

| Field | Values |
|-------|--------|
| `type` | `bug` / `feature` / `task` / `test` |
| `priority` | `p0`–`p3` (impact + urgency) |
| `debt` | `quick-fix` / `balanced` / `proper-refactor` |
| `effort` | `xs` / `s` / `m` / `l` / `xl` |
| `component` | which modules/packages are touched |
| `labels` | type label + priority label |

---

## Step 4 — Write (or update) the plan file

Save to `.claude/plans/{N}-{slug}.md` following the schema in `.claude/plans/README.md`
(if that file exists; otherwise use standard structure: Summary, Acceptance Criteria,
Implementation, Not in Scope, Commit Message, After Implementing checklist).

This file is **temporary** — deleted after the issue is created.

---

## Step 5 — Tech Lead review

Launch the `tech-lead` agent with the plan content. Await verdict:

- **APPROVED** → proceed
- **APPROVED WITH CHANGES** → update plan file, proceed
- **REJECTED** → revise plan file, re-submit

Do not start implementation until Tech Lead approves and user confirms.

---

## Step 6 — Create issue in tracker

Check for duplicates first:
```bash
gh issue list --repo valpere/ragivka --state open --search "<title keywords>"
```

Create using only Summary and Acceptance Criteria (implementation details stay internal):

```bash
gh issue create \
  --repo valpere/ragivka \
  --title "<type>(<component>): <title>" \
  --label "<labels>" \
  --body "$(cat <<'EOF'
## Summary
<summary>

## Acceptance Criteria
<criteria>
EOF
)"
```

Available labels: `bug`, `enhancement`, `documentation`, `good first issue`, `help wanted`, `question`, `wontfix`, `in-review`

### File candidates by component

| Component | Files |
|-----------|-------|
| Observability | `pkg/obs/metrics.go`, `pkg/obs/trace.go`, `pkg/obs/cost.go` |
| Database pool | `pkg/db/db.go` |
| Tenant context | `pkg/tenant/context.go` |
| API server entrypoint | `cmd/server/main.go` |
| Worker entrypoint | `cmd/worker/main.go` |
| Session FSM (planned) | `pkg/runtime/` (Phase 1 step 3) |
| Model Router (planned) | `pkg/aicore/` (Phase 1 step 4) |
| RAG pipeline (planned) | `pkg/knowledge/` (Phase 2) |
| Tool Registry (planned) | `pkg/tools/` (Phase 3) |
| Telegram adapter (planned) | `pkg/channel/` (Phase 4) |
| CI | `.github/workflows/ci.yml` |
| Infrastructure | `docker/docker-compose.yml` |

---

## Step 7 — Delete the plan file

```bash
rm .claude/plans/{N}-{slug}.md
```

The issue is now the canonical record.

---

## Step 8 — Report and stop

Report the created issue URL and confirm plan deletion. Stop.
Implementation is triggered separately via `/ship`.

---

## Output format

```
## Plan: <task name>

**Scope:** <one sentence>
**Type:** bug | feature | task | test
**Priority:** p1: high
**Effort:** s (1–2 hours)

**Files to change:**
- `path/to/file.go` — what changes

**Approach:**
1. ...

**Risks:**
- ...

**Not in scope:**
- ...

---
Issue: #<number> — <url>
Plan file deleted.
```
