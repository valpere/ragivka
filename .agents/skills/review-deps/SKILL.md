---
name: review-deps
description: "Review and triage Dependabot PRs using 6-stage security + stability pipeline. Usage: /review-deps [--dry-run] [PR numbers...] | /review-deps all"
---

# Skill: /review-deps
# Dependabot PR Triage Pipeline

---

## OVERVIEW

```
/review-deps                    → all open Dependabot PRs
/review-deps 175 176 177       → specific PRs by number
/review-deps --dry-run          → preview decisions, no merges/closes
/review-deps --dry-run 176      → dry-run on a specific PR
```

6-stage pipeline per PR:

```
Stage 1: Classify        — patch / minor / major / github-actions
Stage 2: Security check  — scan for CVEs, fast-track if found
Stage 3: Changelog       — fetch release notes, red-flag breaking changes
Stage 4: CI check        — must pass; poll if pending
Stage 5: Lockfile review — unexpected transitive deps, supply-chain signals
Stage 6: Bundle impact   — production vs devDependency
         ↓
Decision: MERGE / BLOCK / CLOSE+TASK / SKIP
         ↓
Post PR comment + execute action (unless --dry-run)
         ↓
Final summary table
```

---

## STEP 0: Parse Arguments

Detect `--dry-run` flag. Set `DRY_RUN=true` and strip it.

Remaining args:
- Empty or `all` → fetch all open Dependabot PRs:
  ```bash
  gh pr list --repo valpere/ragivka --author "app/dependabot" --state open \
    --json number,title,headRefName --limit 50
  ```
- Numeric args → validate each PR is authored by `app/dependabot`

If no open Dependabot PRs: print message and stop.

---

## STEP 1: Per-PR Pipeline

### Stage 1 — Classify

```bash
gh pr view {number} --json title,headRefName,labels
```

Parse title: `Bump {pkg} from {old} to {new}` or `Update {action} from {old} to {new}`

Semver bump type: `patch` / `minor` / `major` (compare first version segment)

Ecosystem detection:
- Branch starts with `dependabot/github_actions/` → `github-actions`
- `go.mod` present in repo root → `go`
- Otherwise → `npm`

**GitHub Actions major bumps** — treat as minor risk (CI tooling only). Skip Stages 3, 5, 6.

**Grouped PRs** — classify by highest bump in the group. Read PR body for individual package versions.

### Stage 2 — Security Check

```bash
gh pr view {number} --json body --jq '.body'
```

Scan for `CVE-\d{4}-\d{4,7}` or `GHSA-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}`.

- critical/high CVE: `CVE_FOUND=high` → fast-track merge after CI passes (skip Stage 3)
- low/moderate CVE: `CVE_FOUND=low` → normal pipeline
- no CVE: `CVE_FOUND=none`

### Stage 3 — Changelog

**Skip if:** `patch` OR `github-actions` OR `CVE_FOUND=high`

Fetch release notes:

```bash
# Try with 'v' prefix first, then without
gh api repos/{org}/{repo}/releases/tags/v{new_version} --jq '.body' 2>/dev/null \
  || gh api repos/{org}/{repo}/releases/tags/{new_version} --jq '.body' 2>/dev/null
```

For `npm` ecosystem, resolve org/repo from the npm registry if unknown:
```bash
# Get repository.url from npm metadata
curl -s "https://registry.npmjs.org/{package_name}/latest" | jq -r '.repository.url'
```

**Known npm → GitHub repo mappings (skip lookup):**

| npm package | GitHub repo |
|-------------|-------------|
| `@supabase/supabase-js` | `supabase/supabase-js` |
| `@tanstack/react-query` | `TanStack/query` |
| `react-router-dom` | `remix-run/react-router` |
| `react-hook-form` | `react-hook-form/react-hook-form` |
| `tailwind-merge` | `dcastil/tailwind-merge` |
| `zod` | `colinhacks/zod` |
| `vite` | `vitejs/vite` |
| `typescript` | `microsoft/TypeScript` |
| `jsdom` | `jsdom/jsdom` |
| `react-resizable-panels` | `bvaughn/react-resizable-panels` |

**Red flags:** "breaking change", "breaking:", "removed support for", "dropped support",
"changed default", "no longer supported", "migration guide", "migration required"

- `CHANGELOG_FLAGS=[list]` if flags found
- `CHANGELOG_FLAGS=unavailable` if fetch fails or no release notes
- `CHANGELOG_FLAGS=none` if clean

### Stage 4 — CI Check

```bash
gh pr checks {number}
```

- All passed → `CI_STATUS=passed`
- Any failed → `CI_STATUS=failed`
- Any pending → poll every 60s, up to 3 retries. If still pending: `CI_STATUS=pending`
- No checks at all → comment `@dependabot rebase`, set `CI_STATUS=no-runs`

### Stage 5 — Lockfile Review

**Skip if:** `github-actions`

**npm:**
```bash
gh pr diff {number} -- package-lock.json 2>/dev/null | head -300
```
Count `^\+\s+"resolved"` lines (new transitive deps):
- patch/minor >5, or major >20 → `LOCKFILE_FLAGS=suspicious`

**go:**
```bash
gh pr diff {number} -- go.sum 2>/dev/null | grep "^+" | wc -l
```
- minor >10 new entries → `LOCKFILE_FLAGS=suspicious`

**Both ecosystems:**
- New `postinstall` script in transitive dep, or typosquatting pattern → `LOCKFILE_FLAGS=supply-chain-risk`
- Otherwise → `LOCKFILE_FLAGS=clean`

### Stage 6 — Bundle Impact

**Skip if:** `github-actions` OR `go`

```bash
node -e "const p=require('./package.json'); const pkg='{package}'; \
  console.log(p.dependencies?.[pkg] ? 'production' : p.devDependencies?.[pkg] ? 'devDep' : 'unknown')"
```

`BUNDLE_TYPE=production|devDep|unknown`

---

## STEP 2: Decision Engine

First match wins:

```
1. CI_STATUS=failed                          → BLOCK
2. CI_STATUS=pending OR no-runs              → SKIP (already rebased if no-runs)
3. LOCKFILE_FLAGS=supply-chain-risk          → BLOCK
4. CVE_FOUND=high AND CI_STATUS=passed       → MERGE (fast-track)
5. patch AND passed AND not supply-chain     → MERGE
6. github-actions AND passed                 → MERGE
7. minor AND passed AND CHANGELOG_FLAGS=none AND LOCKFILE_FLAGS!=suspicious  → MERGE
8. minor AND passed AND (unavailable OR suspicious)  → MERGE with note
9. major AND passed AND CHANGELOG_FLAGS=none → MERGE (comment: no breaking changes detected)
10. major AND passed AND red-flag changelog  → CLOSE + CREATE TASK
11. major AND CHANGELOG_FLAGS=unavailable    → CLOSE + CREATE TASK
12. Fallback                                 → BLOCK
```

---

## STEP 3: Post PR Comment

```bash
gh pr comment {number} --body "## Dependabot Review
| Stage | Result |
|-------|--------|
| Classification | {BUMP_TYPE} · {OLD} → {NEW} |
| Security | {CVE_FOUND} |
| Changelog | {CHANGELOG_FLAGS} |
| CI | {CI_STATUS} |
| Lockfile | {LOCKFILE_FLAGS} |
| Bundle | {BUNDLE_TYPE} |

**Decision: {DECISION}**

{reason}"
```

Skip if `DRY_RUN=true`.

---

## STEP 4: Execute Decision

**MERGE:**
```bash
gh pr merge {number} --merge --auto
```

**BLOCK:** Leave open. Comment already posted.

**CLOSE + CREATE TASK:**
1. Close: `gh pr close {number} --comment "Closing to track major migration separately. Task created."`
2. Create task in project's issue tracker — see project-specific section below.

**SKIP:** Print reason, no action.

---

### Issue Tracker Integration

**CLOSE + CREATE GITHUB ISSUE:**

1. Close the PR:
   ```bash
   gh pr close {number} --repo valpere/ragivka \
     --comment "Closing to track major migration in a GitHub issue."
   ```
2. Create the issue:
   ```bash
   gh issue create \
     --repo valpere/ragivka \
     --title "Migrate {package_name} from v{old_major} to v{new_major}" \
     --label "enhancement" \
     --body "## Context

   Dependabot PR #{number} was closed — major bump needs manual migration.

   **Package:** {package}
   **Current:** {old}
   **Target:** {new}
   **PR:** {url}

   ## Why manual

   {changelog flags or unavailable}

   ## Checklist

   - [ ] Read full migration guide for v{new_major}
   - [ ] Identify breaking changes affecting this codebase
   - [ ] Update usages / imports
   - [ ] Run full test suite: \`go test -race ./...\`
   - [ ] Run security scan: \`govulncheck ./...\`
   - [ ] Verify build passes: \`go build ./...\`
   - [ ] Open PR"
   ```

---

## STEP 5: Final Summary

```markdown
## /review-deps Summary

| PR | Package | Bump | CI | Decision | Action |
|----|---------|------|----|----------|--------|
| #N | {pkg} | {old}→{new} ({type}) | {status} | {decision} | {action} |

**Processed:** N · Merged: M · Blocked: B · Tasks: T · Skipped: S
```

Prefix each action with "(would)" if `DRY_RUN=true`.

---

## RULES

1. **CI must pass before any merge** — no exceptions, even for patches.
2. **Never merge supply-chain-risk lockfile** — manual review required.
3. **Never merge major with breaking-change changelog** — always close + task.
4. **GitHub Actions majors are minor risk** — CI tooling, not application code.
5. **Process sequentially** — each merge changes the lockfile base.
6. **`--dry-run` never modifies anything** — no merges, closes, or comments.
