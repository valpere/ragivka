---
name: task-processor
description: Automates the end-to-end task implementation pipeline (Branch -> Implement -> Test -> Agent Review -> Human Review -> Merge).
---

# Task Processor Workflow

When the USER asks you to implement a task, ship an issue, or process an issue, you MUST autonomously execute the following pipeline from start to finish. Do not ask for permission between steps unless explicitly stated.

## 1. Branch
1. Identify the issue number.
2. Run `gh issue view <number>` to read the exact requirements.
3. Run `git checkout main && git pull` to ensure you are up to date.
4. Run `git checkout -b feat/issue-<number>-<short-description>`.

## 2. Implement
1. Write the Go code, adhering to the project's layout (`cmd/`, `pkg/`, `internal/`) and best practices (Dependency Injection, `context.Context`, idiomatic error wrapping).
2. Update `docs/` in the same branch if architectural changes were made.

## 3. Test
1. Run `go mod tidy`.
2. Run `go build ./...`.
3. Run `go test -v ./...`.
4. Run `golangci-lint run`.
5. Run `gosec ./...`.
6. If any tests or linters fail, automatically fix the errors and re-run this step until everything passes.

## 4. Agent Review
1. Stage and commit your changes: `git add . && git commit -m "feat: implement issue <number>"`
2. Push your branch: `git push -u origin HEAD`
3. Create a Pull Request: `gh pr create --title "feat: <title>" --body "Closes #<number>"`
4. **Agent Self-Review:** Invoke the external agent for a code review by running:
   `codex --dangerously-bypass-approvals-and-sandbox review "Please review the code changes in the current branch against main for security, edge cases, and Go idioms. Return a list of requested changes."`
5. If `codex` requests changes, implement them automatically, amend your commit (`git commit -a --amend --no-edit`), and force push (`git push -f`).

## 5. Human Review
1. Output a markdown message to the USER providing a link to the Pull Request.
2. Provide a summary of the implementation and the results of the `codex` review.
3. **STOP** and explicitly wait for the USER's approval. Do NOT proceed to merge until the USER says "approved", "merge", or "looks good".

## 6. Merge
1. Once the USER approves, merge the PR via CLI: `gh pr merge --squash --delete-branch`
2. Checkout main and pull: `git checkout main && git pull`
3. Inform the user the task is fully shipped.
