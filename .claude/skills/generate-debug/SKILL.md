---
name: generate-debug
description: "Generates a project-specific /debug skill with exact test commands, debug logging setup, and known fragile areas. Run once per project. Usage: /generate-debug"
---

# Skill: /generate-debug
# Generate Project-Specific /debug Skill

Analyzes the current project and writes a customized `/debug` skill to
`.claude/skills/debug/SKILL.md` that knows exact test commands, debug logging
flags, and historically fragile areas.

---

## DISCOVERY STEPS

### Step 1 — Detect stack

```bash
ls package.json go.mod requirements.txt Cargo.toml pom.xml 2>/dev/null | head -5
cat package.json 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('name','?'), d.get('version','?'))" 2>/dev/null
cat go.mod 2>/dev/null | head -5
```

### Step 2 — Find test commands

```bash
# Node/Bun
cat package.json 2>/dev/null | python3 -c "
import sys,json; s=json.load(sys.stdin).get('scripts',{})
for k,v in s.items():
    if 'test' in k.lower() or 'vitest' in v or 'jest' in v: print(f'{k}: {v}')
" 2>/dev/null

# Go
grep -i "test" Makefile 2>/dev/null | head -5
echo "go test ./... (default)"
echo "go test ./... -run TestName (single)"

# Python
grep -i "test" Makefile 2>/dev/null | head -5
cat pyproject.toml 2>/dev/null | grep -A3 "\[tool.pytest"
```

### Step 3 — Find debug logging setup

```bash
# Env var names used in source
grep -roh "os\.Getenv(\"[^\"]*\(LOG\|DEBUG\|VERBOSE\)[^\"]*\")" . \
  --include="*.go" 2>/dev/null | sort -u | head -10
grep -roh "process\.env\.\(LOG\|DEBUG\|VERBOSE\)[A-Z_]*" . \
  --include="*.ts" --include="*.tsx" --include="*.js" 2>/dev/null | sort -u | head -10

# .env.example patterns
grep -iE "^(LOG|DEBUG|VERBOSE|NODE_ENV|GIN_MODE|RUST_LOG)" .env.example .env.local.example 2>/dev/null | head -10
```

### Step 4 — Find fragile areas

```bash
grep -rn "TODO\|FIXME\|HACK\|XXX\|BUG" . \
  --include="*.go" --include="*.ts" --include="*.tsx" \
  --include="*.py" --include="*.rs" --include="*.js" \
  --exclude-dir=node_modules --exclude-dir=.git \
  2>/dev/null | grep -v "_test\." | head -20
```

### Step 5 — Most-changed files (historically fragile)

```bash
git log --name-only --pretty=format: -- \
  '*.go' '*.ts' '*.tsx' '*.py' '*.rs' 2>/dev/null \
  | grep -v '^$' | sort | uniq -c | sort -rn | head -10
```

### Step 6 — Read CLAUDE.md

```bash
cat CLAUDE.md 2>/dev/null | head -60
```

---

## OUTPUT

Write to `.claude/skills/debug/SKILL.md`.

Keep the full generic protocol (Phase 1–5, common patterns, regression test format) intact.
Append a **Project Quick Reference** section:

```markdown
## PROJECT QUICK REFERENCE

### Test commands

Run all  : [exact command]
Run one  : [exact command for a single test by name]
Coverage : [coverage command, or "not configured"]

### Debug logging

[how to enable verbose/debug output for this specific stack]

### Known fragile areas

[top files from most-changed list + TODO/FIXME locations, grouped by theme]

### Stack-specific debug notes

[anything non-obvious: how to inspect edge function logs, how to attach debugger, etc.]
```

---

## RULES

- Write to `.claude/skills/debug/SKILL.md` — overwrite generic if present.
- Keep the generic protocol intact; only add to it, never remove.
- If stack detection is ambiguous, note `[inferred]`.
- After writing, confirm: `Wrote project-specific /debug. Test command: <command>.`
