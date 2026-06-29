---
name: generate-backlog
description: "Generates a project-specific /backlog skill wired to the correct GitHub repo, issue tracker, and architecture file references. Run once per project. Usage: /generate-backlog"
---

# Skill: /generate-backlog
# Generate Project-Specific /backlog Skill

---

## DISCOVERY STEPS

### Step 1 — GitHub repo and issue tracker

```bash
gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null

# ClickUp?
grep -r "clickup\|list_id" .claude/ --include="*.md" --include="*.json" -l 2>/dev/null | head -3

# Labels from existing issues
gh label list --limit 20 2>/dev/null | head -20
```

### Step 2 — Map architecture to file candidates

```bash
cat CLAUDE.md 2>/dev/null | head -80
ls internal/ src/ app/ 2>/dev/null | head -20
```

### Step 3 — Check plans directory schema

```bash
cat .claude/plans/README.md 2>/dev/null | head -40
```

---

## OUTPUT

Write to `.claude/skills/backlog/SKILL.md`.

Keep all 8 steps intact. Make these project-specific changes:

1. **Step 1** — replace `gh issue view <n>` with `gh issue view <n> --repo {GH_REPO}`

2. **Step 2 — file candidates**: replace generic description with a project-specific
   lookup table matching component names to actual file paths:
   ```
   | API / HTTP   | internal/api/handler.go         |
   | Data model   | internal/council/types.go       |
   | Config       | internal/config/config.go       |
   | ...          | ...                             |
   ```

3. **Step 6 — Create issue**: wire to actual tracker:
   - **GitHub Issues**: add `--repo {GH_REPO}` + real label names from Step 1
   - **ClickUp**: replace `gh issue create` with `clickup_create_task` call with
     `list_id`, `assignees`, and formatting matching the project standard

4. **Step 4** — note the plan schema: if `.claude/plans/README.md` exists, reference it;
   otherwise describe the fields used in this project.

---

## RULES

- Write to `.claude/skills/backlog/SKILL.md` — overwrite generic.
- Keep the tech-lead approval gate in Step 5 — do not remove it.
- After writing, confirm: "Wrote /backlog for {project}. Tracker: {tracker}."
