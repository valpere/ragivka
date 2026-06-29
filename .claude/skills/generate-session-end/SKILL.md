---
name: generate-session-end
description: "Installs session recall infrastructure for this project: hooks, settings.local.json, .gitignore entry. Detects available LLMs (agy/opencode) and writes a project-specific /session-end skill. Run once per project. Usage: /generate-session-end"
---

# Skill: /generate-session-end
# Install Session Recall for This Project

Installs the session recall system and writes a project-specific
`/session-end` skill. Run once per project.

What this does:
1. Discovers which LLMs are available (`agy`, `opencode`)
2. Installs hook scripts if not present
3. Updates `.claude/settings.local.json` (creates if missing)
4. Adds `session-log.md` to `.gitignore`
5. Writes a project-specific `.claude/skills/session-end/SKILL.md`
6. Removes mempalace hooks and MCP server (optional, asks first)

---

## DISCOVERY

### Step 1 — Check existing setup

```bash
echo "=== hooks ===" && ls .claude/hooks/session-end.sh .claude/hooks/session-last.sh 2>/dev/null || echo "(not installed)"
echo "=== settings.local.json ===" && cat .claude/settings.local.json 2>/dev/null | python3 -c "import json,sys; d=json.load(sys.stdin); print('Stop hook:', any('session-end' in str(h) for h in d.get('hooks',{}).get('Stop',[])))" 2>/dev/null || echo "(not present)"
echo "=== gitignore ===" && grep session-log .gitignore 2>/dev/null || echo "(not ignored)"
echo "=== existing log ===" && ls -la .claude/session-log.md 2>/dev/null || echo "(no log yet)"
```

### Step 2 — Detect available LLMs

```bash
echo "=== agy ===" && which agy 2>/dev/null && agy models 2>/dev/null | grep -i "gemini\|flash\|pro" | head -10 || echo "(not found)"
echo "=== opencode ===" && which opencode 2>/dev/null && opencode models ollama 2>/dev/null | grep ":cloud" | head -10 || echo "(not found)"
echo "=== claude ===" && which claude 2>/dev/null || echo "(not found)"
```

### Step 3 — Get project name (for log path in troubleshooting)

Must match the derivation `hook-common.sh` uses at runtime (so the generated
troubleshooting line points at the dir the hook actually writes to, from any
subdirectory — not just the repo root):

```bash
basename "$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")"
```

---

## INSTALL

### Step 1 — Copy hook scripts

Source: `~/wrk/common/skills/session-end/hooks/`

If `.claude/hooks/` already has hook scripts (check for `_lib/hook-common.sh`),
skip copying `_lib/` — don't overwrite a project's existing hook infrastructure.

```bash
SRC=~/wrk/common/skills/session-end/hooks

mkdir -p .claude/hooks/_lib

# Copy session hooks
cp "$SRC/session-end.sh"  .claude/hooks/session-end.sh
cp "$SRC/session-last.sh" .claude/hooks/session-last.sh
chmod +x .claude/hooks/session-end.sh .claude/hooks/session-last.sh

# Copy _lib only if not present
[[ ! -f .claude/hooks/_lib/hook-common.sh ]] && \
  cp "$SRC/_lib/hook-common.sh" .claude/hooks/_lib/hook-common.sh
```

Verify syntax:
```bash
bash -n .claude/hooks/session-end.sh && echo "session-end.sh OK"
bash -n .claude/hooks/session-last.sh && echo "session-last.sh OK"
```

### Step 2 — Update settings.local.json

Read current `.claude/settings.local.json` (or `{}`), merge in the two hooks,
write back. Do not remove any existing hooks.

Target structure to merge in:
```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash .claude/hooks/session-end.sh",
            "timeout": 60,
            "statusMessage": "Writing session summary..."
          }
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "bash .claude/hooks/session-last.sh",
            "timeout": 10,
            "statusMessage": "Loading previous session context..."
          }
        ]
      }
    ]
  }
}
```

Merge logic (Python):
```python
import json, os

path = '.claude/settings.local.json'
current = json.load(open(path)) if os.path.exists(path) else {}
hooks = current.setdefault('hooks', {})

new_stop = {"hooks": [{"type": "command", "command": "bash .claude/hooks/session-end.sh", "timeout": 60, "statusMessage": "Writing session summary..."}]}
new_start = {"hooks": [{"type": "command", "command": "bash .claude/hooks/session-last.sh", "timeout": 10, "statusMessage": "Loading previous session context..."}]}

# Append only if not already present
stop_cmds = [h.get('command','') for entry in hooks.get('Stop',[]) for h in entry.get('hooks',[])]
if 'session-end.sh' not in ' '.join(stop_cmds):
    hooks.setdefault('Stop', []).append(new_stop)

start_cmds = [h.get('command','') for entry in hooks.get('SessionStart',[]) for h in entry.get('hooks',[])]
if 'session-last.sh' not in ' '.join(start_cmds):
    hooks.setdefault('SessionStart', []).append(new_start)

json.dump(current, open(path, 'w'), indent=2)
print('settings.local.json updated')
```

### Step 3 — Update .gitignore

If `.gitignore` exists and does not already contain `session-log.md`, append:
```
# Session recall — per-project, local-only context log
.claude/session-log.md
```

---

## WRITE PROJECT-SPECIFIC SKILL

Write `.claude/skills/session-end/SKILL.md` based on discovery results.

Use the generic SKILL.md from `~/wrk/common/skills/session-end/SKILL.md` as
the base. Customize the following sections:

### Customize: LLM fallback chain

Replace the generic "How the full system works" description with the actual
available tools discovered in Step 2. Examples:

**If agy + opencode both available:**
```
LLM fallback chain (Stop hook):
1. agy -p — {list of discovered Gemini models, cheapest first}
2. opencode run --format json — {list of discovered ollama/*:cloud models}
3. Raw transcript excerpt (fallback)
```

**If only agy available:**
```
LLM fallback chain (Stop hook):
1. agy -p — {list of discovered models}
2. Raw transcript excerpt (fallback, opencode not found)
```

**If neither available:**
```
LLM fallback chain (Stop hook):
Stop hook will use raw transcript excerpt only.
Install agy or opencode for LLM-generated summaries.
```

### Customize: troubleshooting path

Replace the generic `~/.cache/$(basename $(pwd))/hooks.log` with the actual
project name resolved from Step 3:
```
~/.cache/{project-name}/hooks.log
```

---

## REMOVE MEMPALACE (optional)

Ask the user:
> "Видалити mempalace з цього проєкту? (хуки + MCP сервер)"

If confirmed, execute the following steps.

### Step 1 — Remove mempalace hooks from settings files

Check both `.claude/settings.json` and `.claude/settings.local.json` for any
hooks that reference `mempalace`. Remove those hook entries. Do not remove
other hooks.

Detection:
```bash
grep -l "mempalace" .claude/settings.json .claude/settings.local.json 2>/dev/null
```

For each file found, read it, remove the mempalace hook entries, write back.

### Step 2 — Remove mempalace MCP server

Check if mempalace is registered:
```bash
claude mcp list 2>/dev/null | grep mempalace
```

If found:
```bash
claude mcp remove mempalace
```

If `claude mcp remove` fails (older CLI version), check
`~/.claude/mcp.json` and remove the `mempalace` entry manually.

### Step 3 — Report mempalace removal status

```
Mempalace removed:
  hooks:      cleared from settings.json / settings.local.json
  MCP server: removed (or "not registered")

Palace data at ~/.mempalace/palace/ was NOT deleted.
Delete manually if no longer needed:
  cp -r ~/.mempalace/palace ~/.mempalace/palace.bak-$(date +%Y%m%d)
  rm -rf ~/.mempalace/palace
```

Do NOT delete the palace data automatically — it may contain history from
other projects. Only report the path and let the user decide.

---

## REPORT

After completing all steps, print a summary:

```
✓ Session recall installed for {project-name}

Hooks:
  .claude/hooks/session-end.sh   — Stop hook (auto-summary on exit)
  .claude/hooks/session-last.sh  — SessionStart hook (injects last entry)

LLM chain:
  1. agy — {models or "not available"}
  2. opencode — {models or "not available"}
  3. raw excerpt (fallback)

Settings:  .claude/settings.local.json ✓
Gitignore: .claude/session-log.md ignored ✓
Skill:     .claude/skills/session-end/SKILL.md ✓

Restart Claude Code to activate hooks.
Run /session-end to write your first entry manually.

Mempalace: {removed / skipped}
```
