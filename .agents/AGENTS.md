# Ragivka Development Workflow & Rules

Every time an agent processes a task or GitHub issue in this repository, they MUST adhere strictly to the following workflow and rules. This file acts as the ultimate source of truth for agent behavior in this workspace.

## 1. Task Processing Workflow (Git & GitHub)
1. **Branching:** Do not commit directly to `main`. Always create a feature branch off of `main` named `feat/issue-<number>-<short-description>` or `fix/issue-<number>-<short-description>`.
2. **Implementation:** Write idiomatic Go code following the project layout and conventions defined below.
3. **Atomic Commits:** Commit code in atomic chunks. Always use conventional commits (e.g., `feat:`, `fix:`, `chore:`, `refactor:`).
4. **Link Issues:** Every commit message that resolves an issue must include `Closes #<number>`.
5. **Pull Requests:** Push the branch to the remote repository and open a Pull Request using `gh pr create`. Do not close issues manually using the CLI; let the merged PR close them automatically.

## 2. Review and Validation Workflow
1. **Self-Review:** Agents must perform a self-review of their diff against the issue requirements before opening a PR.
2. **External Agent Review:** Once a PR is opened, the implementing agent should invoke external agents (e.g., `codex` or `glm-5:cloud` via CLI) to review the diff for security vulnerabilities, edge cases, and adherence to Go idioms.
3. **Security Checks:** Ensure the code complies with `govulncheck` and `gosec`. (This is automated via GitHub Actions).
4. **Human Review:** Await the USER's explicit approval on the Pull Request. If changes are requested by the USER, implement them in follow-up commits.

## 3. Go (Golang) Best Practices
1. **Project Layout:** 
   - `cmd/`: Entry points for binaries.
   - `pkg/`: Public library code.
   - `internal/`: Private application code.
2. **Context:** Pass `context.Context` as the first argument to any function doing I/O (DB calls, API requests, queues) to support cancellation and timeouts (crucial for `NFR-7`).
3. **Error Handling:** Wrap errors with context (e.g., `fmt.Errorf("failed to load user: %w", err)`). Never discard errors silently.
4. **Dependency Injection:** Pass interfaces into structs rather than hardcoding concrete implementations to ensure code is highly modular and easily testable.
5. **Table-Driven Tests:** Write unit tests using the table-driven test pattern.

## 4. Documentation
1. Whenever code changes affect the system architecture, API contracts, or deployment models, update the relevant markdown files in `docs/` in the same PR. Code and docs must ship together.
