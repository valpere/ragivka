# /session-end

Manages `.agents/session-log.md` — a per-project rolling log of session
summaries, one entry per day, last 10 days kept.

```
/session-end          → write today's summary (manual, high quality)
/session-end show     → print the last entry
/session-end all      → print all entries (oldest → newest)
```

---

## /session-end (no args) — write today's summary

Review the current conversation and write today's entry to
`.agents/session-log.md`. Use this before switching projects or closing
Claude Code. The Stop hook (if configured) auto-generates a summary on
exit, but `/session-end` produces better output — full session context,
not transcript extraction.

If an entry for today already exists it is **replaced**, not duplicated.
After write, the log is rotated to keep the last 10 day-entries.

### Entry format

```markdown
## YYYY-MM-DD

### Що зробили
- completed item

### Поточний стан
- current branch / open PR number
- what is working / what is broken

### Відкриті питання
- unresolved question (omit section if none)

### Наступні кроки
- next item, in priority order
```

Rules: 10–20 bullets total · Ukrainian for content · English for code/files/identifiers · include PR number and branch in Поточний стан · omit Відкриті питання if none.

### Write logic

```python
import re, os
from datetime import date

log = '.agents/session-log.md'
today = str(date.today())
max_keep = 10

entry = """## {today}

### Що зробили
- ...

### Поточний стан
- ...

### Наступні кроки
- ...""".format(today=today).strip()

# fill in entry content based on current session, then:

if not os.path.exists(log):
    open(log, 'w').write(entry + '\n')
else:
    content = open(log).read()
    parts = re.split(r'(?m)(?=^## \d{4}-\d{2}-\d{2})', content)
    entries = [p.strip() for p in parts if p.strip()]
    if entries and entries[-1].startswith(f'## {today}'):
        entries[-1] = entry      # replace today
    else:
        entries.append(entry)    # new day
    entries = entries[-max_keep:]
    open(log, 'w').write('\n\n'.join(entries) + '\n')
```

After writing, report:
> "Session log updated — `.agents/session-log.md`, entry for {today}."

---

## /session-end show — print last entry

```python
import re

try:
    with open('.agents/session-log.md') as f:
        parts = re.split(r'(?m)(?=^## \d{4}-\d{2}-\d{2})', f.read())
    entries = [p.strip() for p in parts if p.strip()]
    print(entries[-1] if entries else '(no entries)')
except FileNotFoundError:
    print('No session log found. Run `/session-end` to create one.')
```

---

## /session-end all — print all entries

```python
import re

try:
    with open('.agents/session-log.md') as f:
        parts = re.split(r'(?m)(?=^## \d{4}-\d{2}-\d{2})', f.read())
    entries = [p.strip() for p in parts if p.strip()]
    for e in entries:
        print(e)
        print()
    if entries:
        dates = [e.split('\n')[0].replace('## ', '') for e in entries]
        print(f"{len(entries)} entries: {dates[0]} → {dates[-1]}")
except FileNotFoundError:
    print('No session log found.')
```

---

## How the full system works

```
Session ends (exit / /exit)
  └─ Stop hook: .agents/hooks/session-end.sh   (if configured)
       ├─ Skip if /session-end ran within 2h (file mtime check)
       ├─ Try agy -p  (Gemini 3.5 Flash Low → Medium → High → Gemini 3.1 Pro)
       ├─ Try opencode run --format json  (glm-5:cloud, kimi-k2.5:cloud, minimax-m2.5:cloud, qwen3-coder-next:cloud)
       └─ Fallback: raw transcript excerpt

Next session opens
  └─ SessionStart hook: .agents/hooks/session-last.sh   (if configured)
       ├─ Reads last ## YYYY-MM-DD entry from session-log.md
       ├─ Skips if entry is >30 days old
       └─ Injects as additionalContext: "Previous session context (Xd Yh ago): ..."
```

The skill works standalone (without hooks) — you just run `/session-end`
manually. The hooks add automation.

**Log file**: `.agents/session-log.md` — add to `.gitignore`:
```gitignore
.agents/session-log.md
```

**Setup guide**: hooks are already installed in `.agents/hooks/` and wired
in `.agents/settings.local.json`. No further setup needed.

**Troubleshooting** (if hooks are configured):
```bash
tail -30 ~/.cache/ragivka/hooks.log
cat .agents/session-log.md
```
