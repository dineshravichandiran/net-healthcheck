#!/usr/bin/env bash
# Real test suite for net-healthcheck. No mocks: a real local TCP listener,
# a real local HTTP server, and a real openssl-generated self-signed
# certificate (including one with a deliberately past expiry date) serving
# over a real TLS socket. Every check function actually connects to
# something and gets a genuine answer, the same as it would in production.

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
. "${SCRIPT_DIR}/lib/common.sh"
. "${SCRIPT_DIR}/lib/checks.sh"

PASS=0
FAIL=0
TMPDIR=$(mktemp -d)
PIDS=""

cleanup() {
  for pid in $PIDS; do
    kill "$pid" >/dev/null 2>&1
    wait "$pid" 2>/dev/null
  done
  rm -rf "$TMPDIR"
}
trap cleanup EXIT

assert_status() {
  local desc="$1" expected="$2" actual="$3"
  if [ "$expected" = "$actual" ]; then
    echo "  PASS  $desc"
    PASS=$((PASS + 1))
  else
    echo "  FAIL  $desc (expected $expected, got $actual)"
    FAIL=$((FAIL + 1))
  fi
}

reset_worst() {
  NHC_WORST=$STATUS_OK
  NHC_JSON_RESULTS=""
}

echo "Running tests"
echo ""

# ---------------------------------------------------------------------------
# check_dns
# ---------------------------------------------------------------------------
reset_worst
check_dns "localhost" >/dev/null
assert_status "dns: localhost resolves" "$STATUS_OK" "$NHC_WORST"

reset_worst
check_dns "this-domain-does-not-exist-xyz123.invalid" >/dev/null
assert_status "dns: bogus domain is CRITICAL" "$STATUS_CRITICAL" "$NHC_WORST"

reset_worst
check_dns "127.0.0.1" >/dev/null
assert_status "dns: IP literal short-circuits to OK, not a false CRITICAL" "$STATUS_OK" "$NHC_WORST"

# ---------------------------------------------------------------------------
# check_tcp -- real local listener via python3's http.server on a free port,
# and a real closed port for the refused case.
# ---------------------------------------------------------------------------
find_free_port() {
  python3 -c "
import socket
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.bind(('127.0.0.1', 0))
print(s.getsockname()[1])
s.close()
"
}

HTTP_PORT=$(find_free_port)
python3 -m http.server "$HTTP_PORT" --bind 127.0.0.1 >"${TMPDIR}/http.log" 2>&1 &
HTTP_PID=$!
PIDS="$PIDS $HTTP_PID"; disown $HTTP_PID 2>/dev/null

# Wait for the real listener to actually be accepting connections before
# testing against it, instead of a blind sleep.
for _ in $(seq 1 30); do
  if nc -z 127.0.0.1 "$HTTP_PORT" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

reset_worst
check_tcp "127.0.0.1" "$HTTP_PORT" 2 >/dev/null
assert_status "tcp: real listener is OK" "$STATUS_OK" "$NHC_WORST"

reset_worst
CLOSED_PORT=$(find_free_port)
check_tcp "127.0.0.1" "$CLOSED_PORT" 1 >/dev/null
assert_status "tcp: closed port is CRITICAL" "$STATUS_CRITICAL" "$NHC_WORST"

# ---------------------------------------------------------------------------
# check_http -- against the same real local HTTP server
# ---------------------------------------------------------------------------
reset_worst
check_http "http://127.0.0.1:${HTTP_PORT}/" 200 >/dev/null
assert_status "http: real 200 response is OK" "$STATUS_OK" "$NHC_WORST"

reset_worst
check_http "http://127.0.0.1:${HTTP_PORT}/does-not-exist" 200 >/dev/null
assert_status "http: real 404 vs expected 200 is CRITICAL" "$STATUS_CRITICAL" "$NHC_WORST"

reset_worst
check_http "http://127.0.0.1:${CLOSED_PORT}/" 200 >/dev/null
assert_status "http: connection failure is CRITICAL" "$STATUS_CRITICAL" "$NHC_WORST"

# ---------------------------------------------------------------------------
# check_tls_expiry -- a real openssl-generated cert served over a real TLS
# socket via `openssl s_server`, one valid, one already expired.
# ---------------------------------------------------------------------------
TLS_PORT_OK=$(find_free_port)
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "${TMPDIR}/ok.key" -out "${TMPDIR}/ok.crt" \
  -days 365 -subj "/CN=localhost" >"${TMPDIR}/openssl-ok.log" 2>&1

openssl s_server -accept "$TLS_PORT_OK" -cert "${TMPDIR}/ok.crt" -key "${TMPDIR}/ok.key" \
  -quiet -naccept 100 >"${TMPDIR}/s_server_ok.log" 2>&1 &
TLS_OK_PID=$!
PIDS="$PIDS $TLS_OK_PID"; disown $TLS_OK_PID 2>/dev/null

# A genuinely expired cert: back-date the start so a 1-day validity window
# has already elapsed. faketime isn't guaranteed to be installed, so this
# uses -not_before/-not_after via a config instead of relying on a real
# clock offset.
YESTERDAY_NOT_BEFORE=$(date -v-2d "+%y%m%d%H%M%SZ" 2>/dev/null || date -d "2 days ago" "+%y%m%d%H%M%SZ" 2>/dev/null)
YESTERDAY_NOT_AFTER=$(date -v-1d "+%y%m%d%H%M%SZ" 2>/dev/null || date -d "1 day ago" "+%y%m%d%H%M%SZ" 2>/dev/null)

openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "${TMPDIR}/expired.key" -out "${TMPDIR}/expired.crt" \
  -subj "/CN=localhost" \
  -not_before "$YESTERDAY_NOT_BEFORE" -not_after "$YESTERDAY_NOT_AFTER" \
  >"${TMPDIR}/openssl-expired.log" 2>&1

TLS_PORT_EXPIRED=$(find_free_port)
openssl s_server -accept "$TLS_PORT_EXPIRED" -cert "${TMPDIR}/expired.crt" -key "${TMPDIR}/expired.key" \
  -quiet -naccept 100 >"${TMPDIR}/s_server_expired.log" 2>&1 &
TLS_EXPIRED_PID=$!
PIDS="$PIDS $TLS_EXPIRED_PID"; disown $TLS_EXPIRED_PID 2>/dev/null

for _ in $(seq 1 30); do
  if nc -z 127.0.0.1 "$TLS_PORT_OK" >/dev/null 2>&1 && nc -z 127.0.0.1 "$TLS_PORT_EXPIRED" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

reset_worst
check_tls_expiry "127.0.0.1" "$TLS_PORT_OK" 30 7 >/dev/null
assert_status "tls: real 365-day cert is OK" "$STATUS_OK" "$NHC_WORST"

reset_worst
check_tls_expiry "127.0.0.1" "$TLS_PORT_EXPIRED" 30 7 >/dev/null
assert_status "tls: real expired cert is CRITICAL" "$STATUS_CRITICAL" "$NHC_WORST"

reset_worst
check_tls_expiry "127.0.0.1" "$CLOSED_PORT" 30 7 >/dev/null
assert_status "tls: unreachable port is CRITICAL" "$STATUS_CRITICAL" "$NHC_WORST"

# ---------------------------------------------------------------------------
# JSON mode: output must be valid, parseable JSON
# ---------------------------------------------------------------------------
reset_worst
NHC_JSON_MODE=1
check_dns "localhost" >/dev/null
json_out=$(emit_json_summary)
NHC_JSON_MODE=0
if echo "$json_out" | python3 -c "import json,sys; json.load(sys.stdin)" >/dev/null 2>&1; then
  echo "  PASS  json: output is valid JSON"
  PASS=$((PASS + 1))
else
  echo "  FAIL  json: output is not valid JSON: $json_out"
  FAIL=$((FAIL + 1))
fi

# ---------------------------------------------------------------------------
# End-to-end via the actual bin/ entrypoint, not just the lib functions
# ---------------------------------------------------------------------------
"${SCRIPT_DIR}/bin/net-healthcheck" --host 127.0.0.1 --port "$HTTP_PORT" \
  --url "http://127.0.0.1:${HTTP_PORT}/" --skip-tls >/dev/null 2>&1
bin_exit=$?
assert_status "bin/net-healthcheck: real reachable target exits OK (0)" "0" "$bin_exit"

"${SCRIPT_DIR}/bin/net-healthcheck" --host this-domain-does-not-exist-xyz123.invalid --skip-tls >/dev/null 2>&1
bin_exit=$?
assert_status "bin/net-healthcheck: unreachable target exits CRITICAL (2)" "2" "$bin_exit"

echo ""
echo "passed: $PASS   failed: $FAIL"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
