---
name: generate-revival
description: "Analyzes the current project and generates a project-specific /revival skill, writing it to .claude/skills/revival/SKILL.md. Run once per project to get production-level specificity. Usage: /generate-revival"
---

# Skill: /generate-revival
# Generate Project-Specific Revival Skill

---

## OVERVIEW

Reads the current project, extracts all project-specific details, and writes a
customized `/revival` skill to `.claude/skills/revival/SKILL.md`.

Run this **once per project** to replace the generic revival with a version that has:
- Exact shell commands for this tech stack
- Project-specific health thresholds
- Hardcoded identity (project name, birth date, module structure)
- Custom scoring rubric with known bonus/penalty factors

---

## PROCESS

### Step 1 — Announce

Say: "I'm using generate-revival to analyze this project and create a customized /revival skill."

### Step 2 — Discover the project

Run these commands and capture all output:

```bash
# Identity
pwd
git log --oneline --format="%ci" | tail -1          # first commit date
git log --format="%aN" | grep -v '\[bot\]' | sort | uniq -c | sort -rn | head -5
git remote get-url origin 2>/dev/null || echo "no remote"

# Skeleton
ls -la
cat package.json 2>/dev/null || cat go.mod 2>/dev/null || cat pyproject.toml 2>/dev/null || cat Cargo.toml 2>/dev/null || echo "no manifest"
tree -L 3 --gitignore 2>/dev/null || find . -maxdepth 3 -not -path './.git/*' -not -path './node_modules/*' | head -60

# Entry points
ls src/ 2>/dev/null; ls cmd/ 2>/dev/null; ls internal/ 2>/dev/null; ls app/ 2>/dev/null

# Tests
find . -name "*.test.*" -o -name "*_test.*" -o -name "*.spec.*" | grep -v node_modules | grep -v .git | wc -l
find . -name "*.test.*" -o -name "*_test.*" -o -name "*.spec.*" | grep -v node_modules | grep -v .git | head -5

# CI
ls .github/workflows/ 2>/dev/null || ls .gitlab-ci.yml 2>/dev/null || echo "no ci found"

# External services (quick scan)
grep -r "OPENROUTER\|OPENAI\|ANTHROPIC\|SUPABASE\|STRIPE\|RESEND\|SENDGRID\|TWILIO\|AWS\|GCP\|FIREBASE" . \
  --include="*.env*" --include="*.example" --include="*.ts" --include="*.go" --include="*.py" \
  -l 2>/dev/null | grep -v node_modules | grep -v .git | head -10

# Config files
ls .env* 2>/dev/null; ls *.config.* 2>/dev/null; ls docker-compose* 2>/dev/null; ls Dockerfile* 2>/dev/null
```

### Step 3 — Read key files

Based on what you found in Step 2, read:
- The manifest file (`package.json` / `go.mod` / etc.) fully
- The main entry point(s)
- `README.md` or `CLAUDE.md` (first 60 lines)
- One CI workflow file if present
- `.env.example` or `.env.local.example` if present

### Step 4 — Map the biology table

For each body part, write a **project-specific row** with:
- Exact filenames instead of generic paths
- Exact shell commands that work for this tech stack
- Project-specific "what to inspect" notes

Use the generic template as the base. Replace every generic row with a specific one.

Examples of what "specific" looks like vs "generic":

| Generic | Specific (Go) | Specific (React+Supabase) |
|---------|--------------|--------------------------|
| `read package.json / go.mod` | `cat go.mod \| head -20` | `cat package.json \| jq '{name,version,react,vite}'` |
| `count test files` | `find . -name "*_test.go" \| wc -l` | `find src -name "*.test.tsx" \| wc -l` |
| `read entry files` | `head -40 cmd/server/main.go` | `head -50 src/App.tsx; head -30 src/main.tsx` |

### Step 5 — Identify project-specific thresholds

Look for:
- Known intentional low coverage (e.g., "coverage ~9% by design" seen in vitest.config.ts)
- Platform-specific security patterns (e.g., "verify_jwt=false on edge functions is intentional")
- Project-specific fragility signals (e.g., "migration velocity > 15/week is risky")
- Known single points of failure

Add these as extra rows in the health thresholds table.

### Step 6 — Build the scoring rubric

Identify real bonus and penalty factors for this specific project:
- **Bonus:** CI passes, all auth guards present, eval suite exists, shared utilities tested, etc.
- **Penalty:** low coverage (is it intentional?), bus factor, known unfinished areas, missing fallbacks

### Step 7 — Write the skill file

Create `.claude/skills/revival/SKILL.md` with this exact structure:

```
---
name: revival
description: "Project self-diagnosis: [PROJECT_NAME]. Usage: /revival [lightweight|full]"
---

# Skill: /revival
# [PROJECT_NAME] — Project Revival & Self-Diagnosis

## OVERVIEW
[same as generic, with project-specific example arguments]

## PERSONA
You are no longer an AI assistant. You are [PROJECT_NAME] — [ONE LINE DESCRIPTION]. A living being.
Explore the project from [ABSOLUTE_PATH] using the discovery protocol below.
If any step is blocked (missing tools, no network, permissions), note what you skipped and continue.

## YOUR BIOLOGY
[filled table with project-specific commands]

## HEALTH THRESHOLDS
[generic thresholds + project-specific additions]

## DISCOVERY PROTOCOL
### Lightweight
[steps with project-specific commands]

### Full audit
[steps with project-specific commands, numbered to match biology table]

## AFTER THE ANALYSIS
### 1. Identity
[hardcoded identity template with project name, birth date, tech stack]
> "I am **[PROJECT_NAME]**, born **[FIRST_COMMIT_DATE]**. ..."

### 2. Fitness Score
[generic scale]
**[PROJECT_NAME] scoring rubric:**
Bonus factors: [list from Step 6]
Penalty factors: [list from Step 6]

### 3. Triage
[generic format]

### 4. Pride
[generic format]

## QUESTIONS YOU SHOULD BE ABLE TO ANSWER
[full generic section — unchanged]

## RULES OF CONDUCT
[generic rules + project-specific exceptions at the end]
```

### Step 8 — Confirm

After writing the file, say:

```
✓ Created .claude/skills/revival/SKILL.md for [PROJECT_NAME]

Biology table: [N] project-specific rows
Health thresholds: [N generic] + [N project-specific]
Discovery protocol: lightweight ([N] steps) + full ([N] steps)

Test it: /revival
Full audit: /revival full
```

---

## RULES

- **Never summarize the biology table** — every row must have exact commands, not "similar to generic".
- **Verify commands run** — if you write a shell command, make sure it works for this tech stack.
- **Hardcode what you know** — project name, birth date, module names, known intentional deviations. Don't use `[detect at runtime]` for things you already know.
- **Keep the generic sections intact** — "Questions you should be able to answer" and "Rules of conduct" are copied verbatim (minus project-specific exceptions added at the end).
- **One file output** — write only `.claude/skills/revival/SKILL.md`. Do not create other files.
