---
name: ship
description: "valpere/ragivka ship pipeline: issue → implement → review → merge → close. Usage: user asks to 'ship #ID' or 'ship title'"
---

# Skill: ship
# Issue → Merged PR Pipeline

When the USER asks you to `ship <issue-id>` or `ship <title>`, you MUST autonomously execute the following pipeline from start to finish. Do not stop to ask for permission unless specifically instructed below.

## STEP 0: Resolve Issue & Branch
1. Use `gh issue view <issue-id> --repo valpere/ragivka` to fetch the issue details.
2. Mark it in-progress: `gh issue edit <issue-id> --repo valpere/ragivka --add-label "in-progress"`.
3. Checkout a new branch off main: `git checkout main && git pull && git checkout -b feat/issue-<issue-id>`.

## STEP 0.5: Analyze Task & Resolve Ambiguities
Before writing any code, scan the issue body and codebase for decisions that must be made first. Look for:
- "Decision:", "Options:", "discuss before starting"
- Scope conditionals or dependencies on other issues.
If ambiguities exist, ask the USER with options and tradeoffs. **Wait for their decision before proceeding.** If zero ambiguities, proceed immediately.

## STEP 1: Implement & Pre-flight
1. Write the Go code adhering to the AGENTS.md rules.
2. Update `docs/` in the same branch if architectural changes were made.
3. Run pre-flight checks: `golangci-lint run && go test -race ./...`.
4. If any fail, fix them and repeat until passing.

## STEP 2: Agent Review (/fix-review logic)
1. Commit and push: `git add . && git commit -m "feat: implement issue <issue-id>" && git push -u origin HEAD`.
2. Open PR: `gh pr create --title "feat: issue <issue-id>" --body "Closes #<issue-id>"`
3. Mark in-review: `gh issue edit <issue-id> --repo valpere/ragivka --add-label "in-review" --remove-label "in-progress"`.
4. Spawn a `codex` or `glm-5:cloud` review agent via the CLI to review the PR diff for security, simplicity, and architecture.
5. Apply any fixes requested by the review agents, amend the commit (`git commit --amend --no-edit`), and force push (`git push -f`).

## STEP 3: Merge PR
1. Post a comment on the GitHub Issue summarizing what changed, the file locations, and the tests verified.
2. Ask the USER for explicit approval to merge. **STOP AND WAIT.**
3. Upon approval, merge: `gh pr merge --repo valpere/ragivka --squash --delete-branch`.

## STEP 4: Close Issue & Final Report
1. Close the issue: `gh issue close <issue-id> --repo valpere/ragivka --comment "Shipped in PR."`
2. Print the Final Report to the USER exactly in this format:

```markdown
## /ship complete — #{ISSUE_NUMBER} {ISSUE_TITLE}

Issue:   {ISSUE_URL}  (closed)
PR:      {PR_URL}  (merged, branch deleted)

Pipeline:
✓ Issue resolved + ambiguities cleared
✓ Code implemented
✓ golangci-lint run && go test -race ./... passed
✓ Agent review applied
✓ PR merged (squash)
✓ Completion comment posted
✓ Issue closed
```
