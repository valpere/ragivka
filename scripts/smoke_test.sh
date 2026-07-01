#!/usr/bin/env bash
# End-to-end smoke test for the L1 Customer Support Assistant MVP (Issue #23).
#
# Prerequisites:
#   - docker compose -f docker/docker-compose.yml up -d
#   - OLLAMA_API_KEY exported (Ollama Cloud — see README "Running the MVP")
#   - go run ./cmd/server/ running in another terminal (or SERVER_URL points
#     at an already-running instance)
#
# What it checks:
#   1. /health responds 200
#   2. POST /telegram/webhook/{tenantID} accepts a synthetic Telegram update
#      for the seeded MVP tenant (migrations/006_seed_mvp.sql) and returns 200
#   3. (optional, if psql + DATABASE_URL are available) the assistant's reply
#      was actually persisted with a non-empty answer — the webhook response
#      itself carries no body by design (Telegram expects a fast empty 200;
#      the reply is delivered asynchronously via the Bot API), so this is the
#      most direct way to assert "a real reply was produced" without a live
#      Telegram bot conversation.
set -euo pipefail

SERVER_URL="${SERVER_URL:-http://localhost:8080}"
TELEGRAM_WEBHOOK_SECRET="${TELEGRAM_WEBHOOK_SECRET:-}"
SEED_TENANT_ID="00000000-0000-0000-0000-000000000001"
SEED_CHAT_ID="555000111"
QUESTION="What is your return policy?"

fail() { echo "FAIL: $*" >&2; exit 1; }
info() { echo "-- $*"; }

info "1/3 checking /health"
health_code=$(curl -s -o /dev/null -w '%{http_code}' "${SERVER_URL}/health")
[ "$health_code" = "200" ] || fail "/health returned ${health_code}, want 200"
echo "OK"

info "2/3 posting synthetic Telegram update to the seeded MVP tenant"
update_body=$(cat <<EOF
{
  "update_id": 1,
  "message": {
    "message_id": 1,
    "from": {"id": ${SEED_CHAT_ID}},
    "chat": {"id": ${SEED_CHAT_ID}},
    "text": "${QUESTION}"
  }
}
EOF
)
webhook_code=$(curl -s -o /dev/null -w '%{http_code}' \
  -X POST "${SERVER_URL}/telegram/webhook/${SEED_TENANT_ID}" \
  -H "Content-Type: application/json" \
  -H "X-Telegram-Bot-Api-Secret-Token: ${TELEGRAM_WEBHOOK_SECRET}" \
  -d "${update_body}")
[ "$webhook_code" = "200" ] || fail "webhook returned ${webhook_code}, want 200"
echo "OK"

info "3/3 verifying a non-empty reply was persisted"
if ! command -v psql >/dev/null 2>&1; then
  echo "SKIP (psql not installed — cannot verify persisted reply directly)"
  echo "PASS (1/2 checks; install psql and re-run to verify the reply content)"
  exit 0
fi

DATABASE_URL="${DATABASE_URL:-postgresql://ragivka:ragivka_password@localhost:5432/ragivka_db}"
# Give the synchronous L1 pipeline a moment to complete (retrieval + LLM call).
sleep 2
reply=$(psql "$DATABASE_URL" -tA -c \
  "SELECT content FROM message m
   JOIN session s ON s.id = m.session_id
   WHERE s.tenant_id = '${SEED_TENANT_ID}' AND m.role = 'assistant'
   ORDER BY m.created_at DESC LIMIT 1;" 2>/dev/null || true)

if [ -z "$reply" ]; then
  fail "no assistant reply found in the message table for tenant ${SEED_TENANT_ID}"
fi
echo "OK — reply: ${reply}"

echo "PASS (3/3 checks)"
