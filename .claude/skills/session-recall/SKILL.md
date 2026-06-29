---
name: session-recall
description: "Per-project semantic session history. Indexes Claude session transcripts into a local SQLite DB and injects relevant past context at SessionStart. Replaces mempalace with per-project isolation. Usage: /recall <query>"
---

# /recall

Searches past sessions for this project using semantic similarity (bge-m3) or
BM25 keyword fallback. Requires `session-indexer` in PATH and a local
`.agents/sessions.db` (built by the Stop hook after each session).

```
/recall <query>          → search and display matching session chunks
/recall stats            → show index state (sessions, chunks, embeddings)
```

---

## /recall \<query\>

```bash
PROJECT_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")
DB="$PROJECT_ROOT/.agents/sessions.db"

if [[ ! -f "$DB" ]]; then
  echo "No session index found at $DB"
  echo "The Stop hook (session-index.sh) builds it automatically at session end."
  echo "Run a session and exit to populate the index, then retry."
  exit 0
fi

if ! command -v session-indexer >/dev/null 2>&1; then
  echo "session-indexer not in PATH. See /generate-session-recall for install instructions."
  exit 0
fi

QUERY="$*"
RESULTS=$(session-indexer search "$QUERY" --db "$DB" --limit 10 --json)
python3 - "$RESULTS" <<'PYEOF'
import json, sys, re

results = json.loads(sys.argv[1])
if not results:
    print("No results.")
    sys.exit(0)

print(f"{len(results)} result(s):\n")
for i, r in enumerate(results, 1):
    date = r.get("SessionDate", "?")
    role = r.get("Role", "?")
    score = r.get("Score", 0)
    content = r.get("Content", "").strip()
    # Flag tool call chunks
    is_tool = bool(re.match(r'^(Bash|Read|Write|Edit|Glob|Grep|WebFetch|Agent|Task)\s*\{', content))
    tag = " [tool]" if is_tool else ""
    snippet = content[:400] + ("…" if len(content) > 400 else "")
    print(f"[{i}] {date} · {role}{tag} · score={score:.3f}")
    print(f"    {snippet}")
    print()
PYEOF
```

---

## /recall stats

```bash
PROJECT_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")
DB="$PROJECT_ROOT/.agents/sessions.db"

if [[ ! -f "$DB" ]]; then
  echo "No session index yet."
  exit 0
fi

session-indexer stats --db "$DB"
```

---

## How the full system works

```
Session ends (exit / /exit)
  ├─ session-end.sh    — writes LLM summary to .claude/session-log.md
  └─ session-index.sh  — mines JSONL transcript → .agents/sessions.db (append-only)

Next session opens
  ├─ session-last.sh   — injects last session-log.md entry (structured summary)
  └─ session-recall.sh — searches sessions.db by branch+commit context (semantic)
```

Both SessionStart hooks fire together — `session-last` gives "what we did last
time", `session-recall` gives "what's relevant to current work" across all history.

**Database**: `.agents/sessions.db` — per-project SQLite, gitignored, append-only.
Each session adds chunks; nothing is ever overwritten. This is the key difference
from mempalace (centralised mutable store that corrupted history).

**Search**: bge-m3 vector embeddings when available (requires Ollama + bge-m3 pull),
automatic BM25 FTS5 fallback when embeddings are absent.

**Troubleshooting**:
```bash
# Check hook logs
tail -30 ~/.cache/$(basename "$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")")/hooks.log

# Manual index check
session-indexer stats --db .agents/sessions.db

# Backfill embeddings (if Ollama + bge-m3 available)
session-indexer embed --db .agents/sessions.db
```
