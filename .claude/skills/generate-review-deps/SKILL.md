---
name: generate-review-deps
description: "Generates a project-specific /review-deps skill wired to the correct GitHub repo and issue tracker. Run once per project. Usage: /generate-review-deps"
---

# Skill: /generate-review-deps
# Generate Project-Specific /review-deps Skill

Detects the project's GitHub repo and issue tracker, then writes a customized
`/review-deps` skill to `.claude/skills/review-deps/SKILL.md`.

---

## DISCOVERY STEPS

### Step 1 — GitHub repo

```bash
# From git remote
git remote get-url origin 2>/dev/null

# Or from gh CLI
gh repo view --json nameWithOwner --jq .nameWithOwner 2>/dev/null
```

### Step 2 — Issue tracker

```bash
# ClickUp
grep -r "clickup\|list_id" .claude/ --include="*.md" --include="*.json" -l 2>/dev/null | head -3
grep -r "list_id" .claude/ --include="*.md" -oh '"[0-9]\{9,\}"' 2>/dev/null | head -3
grep -r "assignees\|user.*107\|user.*756\|user.*9166" .claude/ --include="*.md" -h 2>/dev/null | head -5

# Linear
grep -r "linear\|LINEAR_TEAM" .claude/ --include="*.md" -l 2>/dev/null | head -3

# GitHub Issues only (no external tracker)
# If neither ClickUp nor Linear found, default to GitHub Issues
```

### Step 3 — Confirm details

Report what was found:
```
GitHub repo  : {REPO}
Issue tracker: {clickup | linear | github-issues}
ClickUp list : {list_id} (if ClickUp)
ClickUp user : {user_id} (if ClickUp)
```

---

## OUTPUT

Write to `.claude/skills/review-deps/SKILL.md`.

Keep the full generic skill intact. Replace the "Issue Tracker Integration" section with
the project-specific implementation:

**If GitHub Issues:**
```markdown
### Issue Tracker Integration

**CLOSE + CREATE GITHUB ISSUE:**
1. Close the PR:
   ```bash
   gh pr close {number} --comment "Closing to track major migration in a GitHub issue."
   ```
2. Create the issue:
   ```bash
   gh issue create \
     --repo {REPO} \
     --title "Migrate {package_name} from v{old_major} to v{new_major}" \
     --body "## Context\n\nDependabot PR #{number} was closed — major bump needs manual migration.\n\n**Package:** {package}\n**Current:** {old}\n**Target:** {new}\n**PR:** {url}\n\n## Why manual\n\n{changelog flags or unavailable}\n\n## Checklist\n\n- [ ] Read full migration guide for v{new_major}\n- [ ] Identify breaking changes affecting this codebase\n- [ ] Update usages / imports\n- [ ] Run full test suite\n- [ ] Verify build passes\n- [ ] Open PR" \
     --label "dependencies,enhancement"
   ```
```

**If ClickUp:**
```markdown
### Issue Tracker Integration

**CLOSE + CREATE CLICKUP TASK:**
1. Close the PR:
   ```bash
   gh pr close {number} --comment "Closing to track major migration in ClickUp."
   ```
2. Create task:
   ```
   clickup_create_task(
     list_id: "{list_id}",
     name: "Migrate {package_name} from v{old_major} to v{new_major}",
     description: "...",
     assignees: ["{user_id}"],
     priority: "urgent"
   )
   ```
```

Also update:
- `--author "app/dependabot"` Dependabot author (remains the same)
- Add the repo to the STEP 0 `gh pr list` command: `gh pr list --repo {REPO} ...`
- Update frontmatter description with project name

---

## RULES

- Write to `.claude/skills/review-deps/SKILL.md` — overwrite generic if present.
- Keep the full 6-stage pipeline and decision engine intact.
- Only replace the "Issue Tracker Integration" section.
- After writing, confirm: "Wrote project-specific /review-deps for {REPO}. Issue tracker: {tracker}."
