#!/usr/bin/env bash
# Runnable check for the migrations: boots a stock PocketBase on empty data folders and
# asserts the first-boot user, the Italian seed, and that only the console user gets in.
# Usage: PB_BIN=/path/to/pocketbase pb_migrations/check.sh
set -euo pipefail

PB_BIN=${PB_BIN:-pocketbase}
HERE=$(cd "$(dirname "$0")" && pwd)
PORT=${PB_PORT:-18090}
URL=http://127.0.0.1:$PORT
export ADMIN_EMAIL=owner@example.com ADMIN_PASSWORD=console-pass-123
SU_EMAIL=server@example.com SU_PASS=superuser-pass-123
WORK=$(mktemp -d)
PID=
fail() { echo "FAIL: $*" >&2; exit 1; }
stop() { [ -n "$PID" ] && kill "$PID" 2>/dev/null && wait "$PID" 2>/dev/null || true; PID=; }
trap 'stop; rm -rf "$WORK"' EXIT

pb() { "$PB_BIN" --dir "$1" --migrationsDir "$HERE" --hooksDir "$WORK/hooks" --publicDir "$WORK/public" "${@:2}"; }
start() {
  # Run the binary itself in the background so $! is its PID (a function would give a subshell).
  "$PB_BIN" --dir "$1" --migrationsDir "$HERE" --hooksDir "$WORK/hooks" --publicDir "$WORK/public" \
    serve --http "127.0.0.1:$PORT" >"$WORK/serve.log" 2>&1 &
  PID=$!
  for _ in $(seq 50); do curl -fs "$URL/api/health" >/dev/null && return; sleep 0.2; done
  cat "$WORK/serve.log" >&2; fail "PocketBase did not start"
}
token() { curl -fs "$URL/api/collections/$1/auth-with-password" -d "identity=$2&password=$3" | jq -r .token; }
get() { curl -s -H "Authorization: ${2:-}" "$URL/api/collections/$1/records"; }
total() { get "$1" "${2:-}" | jq -r '.totalItems // "refused"'; }

# 1. Empty data folder: boots, creates exactly one user, restart does not add another.
D1=$WORK/d1
pb "$D1" superuser upsert "$SU_EMAIL" "$SU_PASS" >/dev/null
start "$D1"
SU=$(token _superusers "$SU_EMAIL" "$SU_PASS")
[ "$(total users "$SU")" = 1 ] || fail "expected exactly one users record"
stop; start "$D1"
[ "$(total users "$SU")" = 1 ] || fail "restart changed the users count"

# 2. Seed agent: every Italian stack value from the spec.
AGENT=$(get agents "$SU" | jq '.items[0]')
[ "$(get agents "$SU" | jq .totalItems)" = 1 ] || fail "expected one seeded agent"
echo "$AGENT" | jq -e '
  .asr_provider == "aliyun_qwen3" and .asr_config.language == "it" and .asr_config.auto_end == false
  and .llm_provider == "aliyun" and .llm_config.model_name == "qwen3.8-flash" and .llm_config.thinking.mode == "disabled" and .llm_config.max_tokens == 300
  and .tts_provider == "aliyun_qwen" and .tts_config.language_type == "Italian" and (.tts_config.voice | length > 0)
  and (.prompt | length > 0) and .memory_mode == "none" and .voiceprint_enabled == false and .knowledge_enabled == false
  and (.openclaw.enter_keywords | length > 0) and (.openclaw.exit_keywords | length > 0)' >/dev/null || fail "seed agent mismatch: $AGENT"
[ "$(total settings "$SU")" -ge 10 ] || fail "settings not seeded"

# 3. Access: console user sees data; no auth and a second user are refused everywhere.
ME=$(token users "$ADMIN_EMAIL" "$ADMIN_PASSWORD")
[ "$(total agents "$ME")" = 1 ] || fail "console user cannot read agents"
curl -s -H "Authorization: $SU" "$URL/api/collections/users/records" \
  -d "email=second@example.com&password=second-pass-123&passwordConfirm=second-pass-123" >/dev/null
SECOND=$(token users second@example.com second-pass-123)
for c in users agents devices settings commands; do
  for who in "" "$SECOND"; do
    got=$(total $c "$who")
    case $got in 0|refused) ;; *) fail "$c readable by '${who:0:6}': $got";; esac
  done
done
for who in "" "$ME" "$SECOND"; do
  code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: $who" "$URL/api/collections/users/records" \
    -d "email=third@example.com&password=third-pass-123&passwordConfirm=third-pass-123")
  [ "$code" -ge 400 ] || fail "users create allowed ($code)"
done
for c in agents devices settings commands; do
  for who in "" "$SECOND"; do
    code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: $who" "$URL/api/collections/$c/records" -d 'name=x&key=x&device_id=x&code=123456')
    [ "$code" -ge 400 ] || fail "$c writable by '${who:0:6}' ($code)"
  done
done
code=$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: $ME" -H 'Content-Type: application/json' \
  "$URL/api/collections/commands/records" \
  -d '{"type":"settings_reload","status":"pending","payload":{}}')
[ "$code" = 200 ] || fail "console cannot create a command ($code)"
stop

# 4. Migrations repeat on another empty folder.
D2=$WORK/d2
pb "$D2" superuser upsert "$SU_EMAIL" "$SU_PASS" >/dev/null
start "$D2"
SU=$(token _superusers "$SU_EMAIL" "$SU_PASS")
got="$(total users "$SU")/$(total agents "$SU")"
[ "$got" = 1/1 ] || fail "second empty folder differs (users/agents = $got)"

echo "OK"
