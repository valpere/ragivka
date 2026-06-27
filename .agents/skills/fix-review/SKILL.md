---
name: fix-review
description: "Multi-model PR review with parallel fan-out. Primary: OpenRouter (3 models). Automatic failover: Ollama cloud (tier 1) → Ollama local (tier 2). Claude acts as Arbiter — confirms, escalates, or dismisses findings using vote count as confidence signal. Applies a single consolidated fix commit. Auto-merges (squash) when clean. Project-agnostic. Usage: /fix-review [PR-number]"
---

# Skill: /fix-review (parallel)
# User-level multi-model PR review

This is the user-level (`~/.agents/skills/fix-review/`) variant. A
project-level skill at `<repo>/.agents/skills/fix-review/SKILL.md` wins
over this one — Claude Code resolves project skills first.

---

## OVERVIEW

Three model rounds run **in parallel**, then a single Claude arbiter
round adjudicates and applies the consolidated fix.

```
        ┌─→ Round 1 (model A)  ┐
diff ───┼─→ Round 2 (model B)  ┼─→ aggregate (dedupe + vote count)
        └─→ Round 3 (model C)  ┘
                                    ↓
                              Arbiter (Claude) — rules + ONE fix → commit → push
```

**Why parallel:**
- Wall time is `max(t₁, t₂, t₃)` instead of `t₁ + t₂ + t₃`.
- Three independent perspectives on the *same* diff (no caching effect from prior fixes).
- Vote count (1/3, 2/3, 3/3) is a strong confidence signal for the arbiter.
- One fix commit instead of four → cleaner PR history.

**Tradeoff vs. sequential:** no cascading feedback (R2 doesn't see what R1 fixed).
The arbiter handles dedup; in practice most "R2 reacted to R1's fix" cases were
just the second model lagging.

**Merge:** auto-merge (squash + delete branch) when the run is clean —
no fixes were reverted, the PR has no conflicts, and gates either passed
or only failed in layers this PR doesn't touch (diff-scope rule). When
something blocks merge, the skill asks the user once before acting.

---

## RUN COMPLETION CONTRACT (do not skip)

A run is **not** considered complete until **both** of these have happened
in this order, after Step 10 returns:

1. **Step 9 telemetry** — `telemetry.jsonl` has been appended with one
   row per model round + one arbiter row.
2. **Step 11 final summary** — printed to the user.

The auto-merge in Step 10 is **not** the end of the run. After it
returns (merged / left open / closed / asked-and-acted), control MUST
flow into Step 11 and print the summary. Do not stop after merge — the
human reads the summary, not the silence.

---

## STEP 0: Resolve PR + Load Config

```bash
# PR number — argument wins, else current branch's open PR
PR_NUMBER="${1:-$(gh pr view --json number --jq '.number' 2>/dev/null)}"
[ -z "$PR_NUMBER" ] && { echo "No PR found. Pass /fix-review <number>"; exit 1; }

BASE_BRANCH=$(gh pr view "$PR_NUMBER" --json baseRefName --jq '.baseRefName')
```

**Config layering** — project wins over user-level default:

```bash
PROJECT_CONFIG=".agents/skills/fix-review/config.yaml"
USER_CONFIG="$HOME/.agents/skills/fix-review/config.yaml"
CONFIG="$USER_CONFIG"
[ -f "$PROJECT_CONFIG" ] && CONFIG="$PROJECT_CONFIG"
```

Read fields from `$CONFIG` via `yq` (or grep/awk fallback):
- `provider` — `ollama` | `openrouter` | `ask`
- `reviewers.{provider}.round_{1,2,3}.model`
- `{provider}_api_url`
- `post_summary_to_pr`
- `telemetry_enabled`
- `pricing.{model}.{input,output}` — for cost estimation

If `provider: ask`, prompt the user once and persist their choice into
`$CONFIG` via `sed`.

**Load API key** for the active provider:

```bash
source ~/.agents/skills/lib/env.sh
source ~/.agents/skills/lib/rest.sh

case "$PROVIDER" in
  openrouter) load_env_key OPENROUTER_API_KEY; API_KEY="$OPENROUTER_API_KEY" ;;
  ollama)     load_env_key OLLAMA_API_KEY;     API_KEY="$OLLAMA_API_KEY"     ;;
esac
```

If the key is empty after loading, abort — without it the call will fail.

**Provider probe + failover:**

After loading the key, probe the primary provider with a minimal call before
running the full review. If the probe fails (401, 402, "Insufficient credits",
"User not found", network error), fall back to Ollama automatically and
**always print a visible notice to the user**:

```bash
probe_provider() {
  local url="$1" key="$2" model="$3"
  local probe_payload probe_resp err
  probe_payload=$(jq -n --arg m "$model" \
    '{model:$m,messages:[{role:"user",content:"OK"}],stream:false,max_tokens:3}')
  probe_resp=$(curl -s --max-time 10 \
    -H "Content-Type: application/json" \
    ${key:+-H "Authorization: Bearer $key"} \
    -d "$probe_payload" "$url")
  err=$(printf '%s' "$probe_resp" | jq -r '.error.message // ""' 2>/dev/null)
  [ -n "$err" ] && { echo "PROBE_ERROR: $err"; return 1; }
  return 0
}

ACTIVE_PROVIDER="$PROVIDER"
ACTIVE_API_URL="$API_URL"
ACTIVE_KEY="$API_KEY"
ACTIVE_MODELS=("$MODEL_R1" "$MODEL_R2" "$MODEL_R3")
FAILOVER_TIER=""   # "" | "ollama_cloud" | "ollama_local"

OLLAMA_API_URL=$(grep 'ollama_api_url' "$CONFIG" | awk '{print $2}')
load_env_key OLLAMA_API_KEY .env.local 2>/dev/null

read_ollama_models() {
  local tier="$1"  # "ollama_cloud" or "ollama_local" — must match reviewers.* key in config.yaml
  M1=$(yq -r ".reviewers.${tier}.round_1.model" "$CONFIG" 2>/dev/null)
  M2=$(yq -r ".reviewers.${tier}.round_2.model" "$CONFIG" 2>/dev/null)
  M3=$(yq -r ".reviewers.${tier}.round_3.model" "$CONFIG" 2>/dev/null)
}

if [ "$PROVIDER" = "openrouter" ]; then
  if ! probe_provider "$API_URL" "$API_KEY" "$MODEL_R2" 2>&1; then
    echo "⚠️  FAILOVER: OpenRouter unavailable — trying Ollama cloud"
    read_ollama_models "ollama_cloud"
    if probe_provider "$OLLAMA_API_URL" "$OLLAMA_API_KEY" "$M2" 2>&1; then
      ACTIVE_PROVIDER="ollama"; ACTIVE_API_URL="$OLLAMA_API_URL"; ACTIVE_KEY="$OLLAMA_API_KEY"
      ACTIVE_MODELS=("$M1" "$M2" "$M3"); FAILOVER_TIER="ollama_cloud"
      echo "⚠️  FAILOVER: using Ollama cloud (${ACTIVE_MODELS[*]})"
    else
      echo "⚠️  FAILOVER: Ollama cloud unavailable — trying Ollama local"
      read_ollama_models "ollama_local"
      ACTIVE_PROVIDER="ollama"; ACTIVE_API_URL="$OLLAMA_API_URL"; ACTIVE_KEY="$OLLAMA_API_KEY"
      ACTIVE_MODELS=("$M1" "$M2" "$M3"); FAILOVER_TIER="ollama_local"
      echo "⚠️  FAILOVER: using Ollama local (${ACTIVE_MODELS[*]})"
    fi
  fi
fi
```

Use `$ACTIVE_PROVIDER`, `$ACTIVE_API_URL`, `$ACTIVE_KEY`, and `$ACTIVE_MODELS`
everywhere from this point forward. `$FAILOVER_TIER` is `""` when no failover
occurred, `"ollama_cloud"` or `"ollama_local"` otherwise.

**Ollama execution mode:** When `ACTIVE_PROVIDER=ollama`, run the three rounds
**sequentially** (not in parallel) and raise the per-call timeout to 300s.
Local and cloud LLMs can't reliably handle three concurrent large-model calls
without timing out.

In Step 11 final summary, the `### ⚠️ Provider failover` section must name the
tier that was used (`ollama_cloud` or `ollama_local`) and the models.

---

## STEP 1: Detect Project Context

Project-agnosticism is built in. Detect:

### Test command

```bash
detect_test_cmd() {
  if [ -f Makefile ] && grep -qE '^(check|test):' Makefile; then
    if grep -qE '^check:' Makefile; then echo "make check"
    else echo "make test"; fi
  elif [ -f package.json ] && jq -e '.scripts.test' package.json >/dev/null 2>&1; then
    echo "npm test"
  elif [ -f Cargo.toml ];   then echo "cargo test"
  elif [ -f go.mod ];        then echo "go build ./... && go vet ./... && go test ./..."
  elif [ -f pyproject.toml ] || [ -f setup.py ] || [ -f setup.cfg ]; then
    if command -v pytest >/dev/null 2>&1; then echo "pytest -q"
    else echo "python -m pytest -q"; fi
  else echo ""; fi
}
TEST_CMD=$(detect_test_cmd)
```

If `TEST_CMD` is empty, ask the user once: "I can't detect the test command. What should I run?"

### Lint command (best-effort, not blocking)

```bash
detect_lint_cmd() {
  if [ -f Makefile ] && grep -qE '^lint:' Makefile; then echo "make lint"
  elif [ -f package.json ] && jq -e '.scripts.lint' package.json >/dev/null 2>&1; then echo "npm run lint"
  elif [ -f Cargo.toml ]; then echo "cargo clippy -- -D warnings"
  elif [ -f go.mod ];      then echo "golangci-lint run"
  elif [ -f pyproject.toml ] && command -v ruff >/dev/null 2>&1; then echo "ruff check ."
  else echo ""; fi
}
LINT_CMD=$(detect_lint_cmd)
```

### Project context (load-bearing rules to inject into the prompt)

In priority order, take the first that exists:
1. `.agents/context-essentials.md` — purpose-built for this case
2. `CLAUDE.md` (or `GEMINI.md`, `AGENTS.md`) — first ~150 lines, focus on "Key Design Decisions" / "Constraints" / "Banned patterns" sections
3. `README.md` — first ~80 lines as ambient context
4. (none) — generic prompt

```bash
load_project_context() {
  local f
  for f in .agents/context-essentials.md CLAUDE.md GEMINI.md AGENTS.md README.md; do
    if [ -f "$f" ]; then head -150 "$f"; return; fi
  done
  echo ""
}
PROJECT_CONTEXT=$(load_project_context)
```

### Telemetry destination

```bash
TELEMETRY_FILE="$HOME/.agents/skills/fix-review/telemetry.jsonl"
if [ -d ".agents/skills/fix-review" ]; then
  TELEMETRY_FILE=".agents/skills/fix-review/telemetry.jsonl"
fi
```

---

## STEP 2: Build the Review Prompt

Single generic prompt, with `$PROJECT_CONTEXT` injected so each model can
apply project-specific rules.

```
You are a senior code reviewer. Review the following git diff using the
Code Review Pyramid — evaluate from bottom to top, spending the most
attention on lower layers and least on higher ones:

  5 (top)  — Code style        → DO NOT FLAG. Formatters / linters handle this.
  4        — Tests             → Are critical paths covered? New branches tested?
  3        — Documentation     → Is complex logic explained? Public APIs documented?
  2        — Implementation    → Bugs, logic errors, null/nil deref, error handling,
                                 resource leaks, race conditions, security holes,
                                 missing context propagation, performance pitfalls.
  1 (base) — API / Architecture → Layer violations, contract drift, banned patterns,
                                  state-machine violations, hidden coupling.

== Project context ==
{PROJECT_CONTEXT}
== End project context ==

Apply the project rules above to layer 1 and layer 2 findings — they are
load-bearing for this codebase.

Return ONLY a JSON array — no prose, no markdown fences, just raw JSON.
Each item must have exactly these fields:
  "file"     — relative file path (string)
  "line"     — line number on the + side of the diff (integer)
  "layer"    — pyramid layer 1–4 (integer)
  "severity" — "error" | "warning" | "suggestion" (string)
  "body"     — clear description of the issue and how to fix it (string)

Severity guide:
  error      — must fix before merge (bug, security hole, layer-1 violation)
  warning    — should fix (missing test for critical path, undocumented public API)
  suggestion — nice to have

Do NOT flag: formatting, blank lines, import order (layer 5 — automated).
Do NOT flag code not present in this diff.
Do NOT propose architectural rewrites — focus on what the diff actually changes.

If there are no issues, return an empty array: []

Git diff:
---
{DIFF}
---
```

Substitute:
- `{PROJECT_CONTEXT}` — value of `$PROJECT_CONTEXT`
- `{DIFF}` — output of `git diff $(git merge-base HEAD origin/${BASE_BRANCH})...HEAD`

---

## STEP 3: Fan Out — Three Models in Parallel

```bash
DIFF=$(git diff $(git merge-base HEAD origin/${BASE_BRANCH})...HEAD)
PROMPT=$(printf '%s' "$PROMPT_TEMPLATE" | sed "s|{PROJECT_CONTEXT}|$PROJECT_CONTEXT|" | sed "s|{DIFF}|$DIFF|")

# Output dir for this run
RUN_DIR=$(mktemp -d -t fix-review-XXXX)
START_MS=$(python3 -c "import time;print(int(time.time()*1000))" 2>/dev/null || echo $(($(date +%s) * 1000)))

# Fire all three in parallel — each writes its raw response to a file.
# System message enforces JSON-only output even when the diff contains markdown
# prose (spec/plan files) that confuses models into writing analysis text.
REVIEW_SYSTEM_MSG="You are a senior code reviewer. Your entire response MUST be a raw JSON array — nothing else. Start with [ and end with ]. No prose, no markdown fences, no explanations before or after. If there are no issues output exactly: []"
export REVIEW_SYSTEM_MSG

run_round() {
  local n="$1" model="$2"
  local r_start r_end
  r_start=$(python3 -c "import time;print(int(time.time()*1000))" 2>/dev/null || echo $(($(date +%s) * 1000)))
  local payload response active_provider active_url active_key
  active_provider="$PROVIDER"
  active_url="$API_URL"
  active_key="$API_KEY"
  payload=$(chat_payload_system "$active_provider" "$model" "$REVIEW_SYSTEM_MSG" "$PROMPT")
  response=$(rest_post "$active_url" "$payload" "$active_key") || response='{"error":"call failed"}'

  # Failover chain: OpenRouter → Ollama cloud → Ollama local.
  # Each tier is tried only if the previous one returned an error response.
  local err_code ollama_api_url
  ollama_api_url=$(yq -r '.ollama_api_url' "$CONFIG" 2>/dev/null || grep '^ollama_api_url:' "$CONFIG" | awk '{print $2}')
  load_env_key OLLAMA_API_KEY .env.local 2>/dev/null

  err_code=$(printf '%s' "$response" | jq -r '.error.code // empty' 2>/dev/null)
  if [ -n "$err_code" ] && [ "$active_provider" = "openrouter" ]; then
    # Tier 1 failover: Ollama cloud (config section: reviewers.ollama_cloud)
    local cloud_model
    cloud_model=$(yq -r ".reviewers.ollama_cloud.round_${n}.model" "$CONFIG" 2>/dev/null)
    echo "warn: round ${n} OpenRouter error (code=${err_code}) — trying Ollama cloud (${cloud_model})" >&2
    active_provider="ollama"
    active_url="$ollama_api_url"
    active_key="$OLLAMA_API_KEY"
    payload=$(chat_payload_system "$active_provider" "$cloud_model" "$REVIEW_SYSTEM_MSG" "$PROMPT")
    response=$(rest_post "$active_url" "$payload" "$active_key") || response='{"error":"ollama cloud failover failed"}'
    model="$cloud_model"

    err_code=$(printf '%s' "$response" | jq -r '.error.code // empty' 2>/dev/null)
    if [ -n "$err_code" ]; then
      # Tier 2 failover: Ollama local
      local local_model
      local_model=$(yq -r ".reviewers.ollama_local.round_${n}.model" "$CONFIG" 2>/dev/null)
      echo "warn: round ${n} Ollama cloud error (code=${err_code}) — trying Ollama local (${local_model})" >&2
      payload=$(chat_payload_system "$active_provider" "$local_model" "$REVIEW_SYSTEM_MSG" "$PROMPT")
      response=$(rest_post "$active_url" "$payload" "$active_key") || response='{"error":"ollama local failover failed"}'
      model="$local_model"
    fi
  fi

  r_end=$(python3 -c "import time;print(int(time.time()*1000))" 2>/dev/null || echo $(($(date +%s) * 1000)))
  printf '%s' "$response"   > "$RUN_DIR/round_${n}.raw.json"
  printf '%s\n%s' "$model"  "$((r_end - r_start))" > "$RUN_DIR/round_${n}.meta"
}

run_round 1 "$MODEL_R1" &
run_round 2 "$MODEL_R2" &
run_round 3 "$MODEL_R3" &
wait
```

> **Note on `&` + `wait`**: each background job runs in a subshell, so any
> exported variables from `source` calls remain visible. The functions
> defined above must be exported too — do `export -f run_round chat_payload chat_payload_system chat_content rest_post ollama_payload openrouter_payload openrouter_payload_system ollama_content openrouter_content` once before the parallel block, or inline the body of `run_round` in `bash -c '...' &` calls.
>
> **Failover chain**: `run_round()` cascades OpenRouter → Ollama cloud → Ollama local on per-round errors. Cloud models come from `reviewers.ollama_cloud.round_N.model`; local models from `reviewers.ollama_local.round_N.model`. `OLLAMA_API_KEY` is loaded from `.env.local`. If Ollama local also fails, the round produces `[]` (treated as 0 findings). Failover is only attempted when `PROVIDER=openrouter`; if `PROVIDER=ollama` fails, there is no secondary.

---

## STEP 4: Parse Each Response → Findings Array

For each round 1, 2, 3:

```bash
parse_round() {
  local n="$1"
  local raw content
  raw=$(cat "$RUN_DIR/round_${n}.raw.json")
  content=$(chat_content "$PROVIDER" "$raw")
  # Strip code fences if model wrapped despite instructions.
  content=$(printf '%s' "$content" | sed -E 's/^```(json)?//; s/```$//')
  # Validate as JSON array.
  if ! echo "$content" | jq -e 'type == "array"' >/dev/null 2>&1; then
    echo "[]" > "$RUN_DIR/round_${n}.findings.json"
    echo "warn: round ${n} response not a JSON array — treating as 0 findings" >&2
    return
  fi
  echo "$content" > "$RUN_DIR/round_${n}.findings.json"
}
parse_round 1; parse_round 2; parse_round 3
```

If a round returned prose: skip its findings (already 0). Don't retry —
parallel mode runs once.

---

## STEP 5: Aggregate — Dedupe + Vote Count

Merge all three rounds. Dedupe by `(file, line)`. For each unique location,
record:
- `votes` — how many models flagged it (1, 2, or 3)
- `models` — which models
- `body` — concatenation or longest body (longest is usually most informative)
- `worst_severity` — `error` > `warning` > `suggestion`
- `min_layer` — lowest pyramid layer mentioned (lower = more critical)

```bash
jq -s '
  # input: array of three arrays
  flatten
  | group_by(.file + ":" + (.line|tostring))
  | map({
      file:     .[0].file,
      line:     .[0].line,
      votes:    length,
      models:   [.[] | .model // ""],
      bodies:   [.[] | .body],
      body:     ([.[] | .body] | sort_by(length) | last),
      severity: ([.[] | .severity] | unique | (if any(. == "error") then "error" elif any(. == "warning") then "warning" else "suggestion" end)),
      layer:    ([.[] | .layer]    | min)
    })
  | sort_by(.layer, (if .severity == "error" then 0 elif .severity == "warning" then 1 else 2 end), -.votes)
' "$RUN_DIR/round_1.findings.json" "$RUN_DIR/round_2.findings.json" "$RUN_DIR/round_3.findings.json" \
  > "$RUN_DIR/aggregated.json"
```

> jq doesn't carry the `model` field through unless we add it during parse;
> for accurate `models[]`, tag findings with their model in Step 4 before
> aggregation: pipe each `findings.json` through `jq --arg m "$MODEL_RX" 'map(. + {model:$m})'` first.

The aggregated file is sorted critical-first: layer 1 errors found by 3/3 → layer 4 suggestions found by 1/3.

---

## STEP 6: Arbiter (Claude)

Read `$RUN_DIR/aggregated.json`. For each finding, rule:

| Ruling | When |
|---|---|
| **CONFIRM** | Real issue. Default for `votes ≥ 2` unless clearly false-positive. |
| **ESCALATE** | Real issue, more severe than tagged. |
| **DISMISS** | False positive, conflicts with project context, or layer-5 noise. Default for `votes == 1` unless obviously real. |
| **DEFER** | Real but out of scope. Log, don't fix. |

**Vote count is a confidence prior, not a verdict.** A 1-vote finding
that obviously points to a real bug should be confirmed. A 3-vote
finding that contradicts the project's load-bearing rules
(`.agents/context-essentials.md` / `CLAUDE.md`) should be dismissed
with reason.

**Independent scan**: also walk the full diff once and flag anything
all three models missed (rare but high-value).

**Apply CONFIRM + ESCALATE fixes** via the Edit tool. Minimal change
per fix; no opportunistic refactoring.

**Run gates** after fixes:
```bash
$TEST_CMD 2>&1 | tail -30
[ -n "$LINT_CMD" ] && $LINT_CMD 2>&1 | tail -20
```

### Diff-scope check before reverting

A gate failure does not automatically mean a fix is at fault. Before
reverting:

1. **Check what files this PR actually touches** —
   `git diff --name-only $(git merge-base HEAD origin/${BASE_BRANCH})...HEAD`.
2. **Map failure to file types**:
   - Build / type-check / unit-test failure → caused only by changes to
     source files of that language (`.ts`/`.tsx`, `.go`, `.rs`, `.py` etc.).
   - Lint failure → same source files plus lint config.
   - Dependency CVE / vulnerability scan → only if dep manifest changed
     (`package.json`/`go.mod`/`Cargo.toml`/`pyproject.toml` + lock).
   - Migration / schema test → only if migration files changed.
3. **Decide**:
   - If the failing layer **is touched** by the diff → identify which fix
     is at fault, revert it via Edit, log as
     `reverted — caused $TEST_CMD failure`. Re-run gates.
   - If the failing layer **is NOT touched** by the diff → mark as
     **pre-existing**. Log in summary as
     `Pre-existing failure — not introduced by this PR. Continuing.`
     Do **not** revert. Do **not** silently pass the gate either —
     surface it explicitly so the human knows.

In particular, a documentation/config/skill-only PR (changes confined to
docs, `.agents/`, README, CI YAML) cannot cause source-build failures
by construction. Gates that fail in that case are always pre-existing.

Set `GATES_OK` for the merge step to read:

```bash
# After all gates run + reverts processed:
#   "yes" — passed clean OR every failure was in-scope-skipped as pre-existing
#   "no"  — at least one failing layer is touched by this PR's diff
GATES_OK=yes  # default optimistic; flip to "no" if any in-scope failure remains
```

---

## STEP 7: Commit + Push (single commit)

```bash
git add -A
# Don't commit telemetry if it's project-local
git restore --staged .agents/skills/fix-review/telemetry.jsonl 2>/dev/null || true

git commit -m "fix(pr#${PR_NUMBER}): address /fix-review findings

$(jq -r '.[] | select(.ruling=="CONFIRM" or .ruling=="ESCALATE") | "- \(.file):\(.line) — \(.body[0:80])"' "$RUN_DIR/arbiter.json" | head -20)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"

git push
```

Skip the commit entirely if zero fixes applied. (Models may have all
agreed everything was fine.)

---

## STEP 8: Optional PR Summary Comment

If `post_summary_to_pr: true`:

```bash
gh pr comment "$PR_NUMBER" --body "$(cat <<EOF
<details>
<summary>/fix-review — ${PROVIDER} parallel pass · ${TOTAL_FINDINGS} findings · ${CONFIRMED} fixed · ${DISMISSED} dismissed</summary>

| File:Line | Votes | Layer | Sev | Ruling | Note |
|-----------|-------|-------|-----|--------|------|
${TABLE_ROWS}

Models: ${MODEL_R1}, ${MODEL_R2}, ${MODEL_R3}
Arbiter: Claude (vote count used as confidence prior)
</details>
EOF
)"
```

---

## STEP 9: Telemetry — JSONL Append

One entry per round + one arbiter entry, all in the same run.

**Round entry:**
```jsonc
{
  "timestamp": "2026-05-10T12:34:56Z",
  "pr_number": 42,
  "round_number": 1,
  "model": "deepseek/deepseek-v3.2",
  "provider": "openrouter",
  "findings_count": 4,
  "prompt_tokens": 12000,
  "completion_tokens": 600,
  "estimated_cost_usd": 0.003348,
  "duration_ms": 8200
}
```

**Arbiter entry:**
```jsonc
{
  "timestamp": "2026-05-10T12:35:10Z",
  "pr_number": 42,
  "round_number": "arbiter",
  "model": "claude",
  "provider": "local",
  "confirmed": 5,
  "escalated": 1,
  "dismissed": 3,
  "added_new": 1,
  "parallel": true,
  "wall_time_ms": 9100
}
```

`wall_time_ms` is the parallel block duration (Step 3 START_MS → end of `wait`),
so you can compare against sum of round `duration_ms` to see the speedup.

`duration_ms` only appears on **round rows** and means pure model API
call time (HTTP request → response). The **arbiter row** has no
`duration_ms` field — local Claude has no API boundary, and the running
session does not reliably execute bash blocks meant to capture start/
end timestamps across multiple Bash tool invocations. Use
`wall_time_ms` (parallel block duration) as the timing signal in the
arbiter row.

**Append (fail-open):**
```bash
if [ "$TELEMETRY_ENABLED" = "true" ]; then
  jq -cn ... '{...}' >> "$TELEMETRY_FILE" 2>/dev/null || \
    echo "warn: telemetry write failed — continuing" >&2
fi
```

Cost estimation:
```bash
INPUT_PRICE=$(yq -r ".pricing[\"$MODEL\"].input  // \"null\"" "$CONFIG")
OUTPUT_PRICE=$(yq -r ".pricing[\"$MODEL\"].output // \"null\"" "$CONFIG")
if [ "$PROMPT_TOKENS" != "null" ] && [ "$INPUT_PRICE" != "null" ]; then
  COST=$(echo "scale=8; ($PROMPT_TOKENS / 1000000) * $INPUT_PRICE + ($COMPLETION_TOKENS / 1000000) * $OUTPUT_PRICE" | bc)
fi
```

---

## STEP 10: Auto-merge (or ask if blocked)

Default: **merge silently when clean**. Only prompt the user if something
blocks merge.

Conditions for clean auto-merge — all three must hold:

1. **No reverts** — `arbiter.json` has no `"reverted"` rulings.
2. **PR mergeable** — no merge conflicts.
3. **Gates green or pre-existing-skip** — Step 6 either passed cleanly,
   or every failing layer was confirmed pre-existing per the diff-scope
   rule (so `GATES_OK="yes"`).

```bash
MERGEABLE=$(gh pr view "$PR_NUMBER" --json mergeable --jq '.mergeable')
HAS_REVERT=$(jq -e 'any(.[]?; .ruling == "reverted")' "$RUN_DIR/arbiter.json" >/dev/null 2>&1 && echo "yes" || echo "no")
GATES_OK="${GATES_OK:-yes}"

BLOCKING=()
[ "$MERGEABLE" != "MERGEABLE" ] && BLOCKING+=("PR not mergeable: ${MERGEABLE}")
[ "$HAS_REVERT" = "yes" ]       && BLOCKING+=("one or more fixes reverted")
[ "$GATES_OK"  != "yes" ]       && BLOCKING+=("local gates failed in a layer this PR touches")

if [ ${#BLOCKING[@]} -eq 0 ]; then
  if ! gh pr merge "$PR_NUMBER" --auto --squash --delete-branch 2>/dev/null; then
    gh pr merge "$PR_NUMBER" --squash --delete-branch
  fi
  MERGE_STATUS="merged (squash)"
else
  cat <<EOM
PR #${PR_NUMBER} cannot auto-merge:
$(printf '  - %s\n' "${BLOCKING[@]}")

What should I do?
  1. Merge anyway (squash + delete branch)
  2. Leave open — you'll handle it manually
  3. Close PR — abandon
EOM
  # Wait for the user's choice and act:
  #   1 → gh pr merge "$PR_NUMBER" --squash --delete-branch ; MERGE_STATUS="merged (forced)"
  #   2 → MERGE_STATUS="left open"
  #   3 → gh pr close "$PR_NUMBER" --delete-branch ; MERGE_STATUS="closed"
fi
```

Notes:
- `--auto` requires "Allow auto-merge" in repo Settings → General. If
  unavailable the fallback `gh pr merge --squash` runs immediately.
- `--delete-branch` keeps the remote tidy.
- The user prompt fires only when truly necessary — clean runs are silent
  squash-merges.

**→ Now proceed to Step 11.** A successful merge does **not** end the
run — the user still needs the final summary. See *Run Completion
Contract* near the top of this skill.

---

## STEP 11: Final Summary (printed)

```
## /fix-review (parallel) — PR #${PR_NUMBER}

Provider: ${ACTIVE_PROVIDER}
Models:   ${MODEL_R1} | ${MODEL_R2} | ${MODEL_R3}
Wall time: ${WALL_TIME_MS} ms (vs sum-sequential ${SUM_SEQ_MS} ms — ${SPEEDUP}× speedup)

Aggregated findings: ${TOTAL}
  3/3 votes: ${THREE_VOTE} (high confidence)
  2/3 votes: ${TWO_VOTE}
  1/3 votes: ${ONE_VOTE}

Arbiter:
  Confirmed: ${CONFIRMED}
  Escalated: ${ESCALATED}
  Dismissed: ${DISMISSED}
  Deferred:  ${DEFERRED}
  Added new: ${ADDED_NEW}

Tests: ${TEST_RESULT}
Lint:  ${LINT_RESULT}

Commit: ${COMMIT_SHA}
PR:     ${PR_URL}
Merge:  ${MERGE_STATUS}        # merged | merged (forced) | left open | closed
Telemetry: ${TELEMETRY_FILE}
```

**Failover reporting (mandatory):** If `FAILOVER_TIER` is non-empty, always append a
`### ⚠️ Provider failover` section immediately after the summary block:

```
### ⚠️ Provider failover

Primary provider (openrouter) was unavailable.
Tier used: ${FAILOVER_TIER}   # "ollama_cloud" = Ollama cloud (tier 1), "ollama_local" = Ollama local (tier 2)
Reason: ${FAILOVER_REASON}    # e.g. "402 Insufficient credits", "network error"
Models: ${M1} | ${M2} | ${M3}

Action required: top up OpenRouter credits or switch provider in config.yaml.
```

This section is **never omitted** when failover occurred. Bury it in the
provider line and the user will miss it.

---

## SWITCHING PROVIDERS

User-level config: edit `~/.agents/skills/fix-review/config.yaml`.
Project-level override: drop a `.agents/skills/fix-review/config.yaml`
in the repo root (project wins).

Or ask: "switch fix-review to openrouter" / "switch fix-review to ollama".

---

## DIFFERENCES FROM SEQUENTIAL VARIANT

If a project carries a sequential variant (e.g. lance-agent), keep it
there — this user-level skill is the parallel fan-out. Differences:

| | Sequential (project) | Parallel (this) |
|--|--|--|
| Wall time | t₁+t₂+t₃ | max(t₁,t₂,t₃) |
| Commits per run | up to 4 (3 rounds + arbiter) | 1 |
| Cascading fixes | yes (R2 sees R1's fixes) | no |
| Vote count | implicit (last to flag wins) | explicit (1/3, 2/3, 3/3) |
| Loop detection | yes (80% overlap stop) | n/a — only one pass |
| Token cost | same | same |

When in doubt: **prefer this parallel variant** for clean PR history and
faster wall time. Use sequential only when you specifically want
cascading review (rare).
