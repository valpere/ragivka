#!/bin/bash
# SessionStart hook: injects semantically relevant past session context.
# Derives a search query from git branch + recent commits, searches the
# per-project session index (.agents/sessions.db), and injects matching
# chunks as additionalContext.
#
# Complements session-last.sh (which injects the last structured entry).
# This hook adds semantic recall across all indexed sessions.

set -uo pipefail

source "$(dirname "$0")/_lib/hook-common.sh"
hook_setup_logging "session-recall.sh"

if ! command -v session-indexer >/dev/null 2>&1; then
    echo "[$(date -Iseconds)] session-recall: session-indexer not in PATH, skipping" >> "$LOG_FILE"
    exit 0
fi

PROJECT_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")
DB="$PROJECT_ROOT/.agents/sessions.db"

if [[ ! -f "$DB" ]]; then
    echo "[$(date -Iseconds)] session-recall: no sessions.db yet, skipping" >> "$LOG_FILE"
    exit 0
fi

# Derive search query from git context (branch name + recent commit messages)
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null | sed 's/[_\/-]/ /g' || echo "")
COMMITS=$(git log --oneline -3 2>/dev/null | cut -d' ' -f2- | tr '\n' ' ' || echo "")
QUERY="$(printf '%s %s' "$BRANCH" "$COMMITS" | tr -s ' ' | sed 's/^ //;s/ $//')"

if [[ -z "$QUERY" ]]; then
    echo "[$(date -Iseconds)] session-recall: empty query, skipping" >> "$LOG_FILE"
    exit 0
fi

echo "[$(date -Iseconds)] session-recall: query='${QUERY:0:80}'" >> "$LOG_FILE"

RESULTS=$(session-indexer search "$QUERY" --db "$DB" --limit 5 --json 2>/dev/null || echo "[]")

CONTEXT=$(python3 - "$RESULTS" <<'PYEOF'
import json, sys, re

try:
    results = json.loads(sys.argv[1])
except Exception:
    sys.exit(0)

if not results:
    sys.exit(0)

by_date = {}
for r in results:
    date = r.get("SessionDate", "unknown")
    content = r.get("Content", "").strip()
    score = r.get("Score", 0)

    # Skip tool call noise — raw JSON blobs from Bash/Read/Write/etc.
    if re.match(r'^(Bash|Read|Write|Edit|Glob|Grep|WebFetch|WebSearch|Agent|Task)\s*\{', content):
        continue

    # Truncate long chunks
    snippet = content[:280] + ("…" if len(content) > 280 else "")
    by_date.setdefault(date, []).append((score, snippet))

if not by_date:
    sys.exit(0)

lines = []
for date in sorted(by_date.keys(), reverse=True)[:3]:
    lines.append(f"### {date}")
    for score, snippet in sorted(by_date[date], reverse=True)[:2]:
        lines.append(f"  [{score:.2f}] {snippet}")
    lines.append("")

print("\n".join(lines).strip())
PYEOF
)

if [[ -z "$CONTEXT" ]]; then
    echo "[$(date -Iseconds)] session-recall: no usable results after filtering" >> "$LOG_FILE"
    exit 0
fi

echo "[$(date -Iseconds)] session-recall: injecting context" >> "$LOG_FILE"

jq -n --arg ctx "$CONTEXT" '{
  hookSpecificOutput: {
    hookEventName: "SessionStart",
    additionalContext: ("Relevant past sessions (semantic search):\n\n" + $ctx)
  }
}'
