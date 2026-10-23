#!/usr/bin/env bash
# Runs the Bash tool and the Go tool against the SAME targets and compares what they report.
# Targets are local (real HTTP server, real openssl TLS servers with real certificates of different
# ages, a closed port) plus one real internet name. Needs: go, openssl, python3, nc.
set -u
cd "$(dirname "$0")/../.."          # repo root
GO_BIN="$(mktemp -d)/nhc-go"
( cd go && go build -o "$GO_BIN" ./cmd/net-healthcheck ) || { echo "go build failed"; exit 1; }
BASH_BIN="./bin/net-healthcheck"

T="$(mktemp -d)"; PIDS=""
cleanup() { for p in $PIDS; do kill "$p" 2>/dev/null; done; rm -rf "$T"; }
trap cleanup EXIT
free_port() { python3 -c 'import socket;s=socket.socket();s.bind(("127.0.0.1",0));print(s.getsockname()[1])'; }
date_off() { date -v"$1" "+%y%m%d%H%M%SZ" 2>/dev/null || date -d "$2" "+%y%m%d%H%M%SZ"; }

# A real HTTP server: /ok 200, /missing 404, /redirect 302.
HP=$(free_port)
python3 - "$HP" >/dev/null 2>&1 <<'PY' &
import sys, http.server
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/ok": self.send_response(200); self.end_headers()
        elif self.path == "/redirect": self.send_response(302); self.send_header("Location", "/ok"); self.end_headers()
        else: self.send_response(404); self.end_headers()
    def log_message(self, *a): pass
http.server.HTTPServer(("127.0.0.1", int(sys.argv[1])), H).serve_forever()
PY
PIDS="$PIDS $!"; disown $! 2>/dev/null

# Real certificates with different remaining lifetimes, each served by a real openssl s_server.
mkcert() { # name not_before_offset not_after_offset
  openssl req -x509 -newkey rsa:2048 -nodes -keyout "$T/$1.key" -out "$T/$1.crt" -subj "/CN=localhost" \
    -not_before "$(date_off "$2" "$3")" -not_after "$(date_off "$4" "$5")" >/dev/null 2>&1
  local port; port=$(free_port)
  openssl s_server -accept "$port" -cert "$T/$1.crt" -key "$T/$1.key" -quiet -naccept 200 >/dev/null 2>&1 &
  PIDS="$PIDS $!"; disown $! 2>/dev/null; echo "$port"
}
P_OK=$(mkcert ok -2d "2 days ago" +365d "365 days")
P_WARN=$(mkcert warn -2d "2 days ago" +20d "20 days")
P_CRIT=$(mkcert crit -2d "2 days ago" +3d "3 days")
P_EXP=$(mkcert expired -5d "5 days ago" -1d "1 day ago")
P_CLOSED=$(free_port)
for p in $HP $P_OK $P_WARN $P_CRIT $P_EXP; do for _ in $(seq 1 40); do nc -z 127.0.0.1 "$p" 2>/dev/null && break; sleep 0.25; done; done

statuses() { grep -E '^\[' | sed -E 's/^\[([A-Z]+) *\] +([^ ]+).*/\2=\1/' | tr '\n' ' '; }

printf '%-34s %-9s %-9s %s\n' "scenario" "bash" "go" "same per-check statuses?"
printf '%-34s %-9s %-9s %s\n' "--------" "----" "--" "------------------------"
same=0; total=0
scenario() { # title, args...
  local title="$1"; shift
  local b g be ge
  b=$("$BASH_BIN" "$@" 2>/dev/null); be=$?
  g=$("$GO_BIN" "$@" 2>/dev/null); ge=$?
  local bs gs; bs=$(echo "$b" | statuses); gs=$(echo "$g" | statuses)
  # The two tools label the same check slightly differently only in the free-text message, never in name or status.
  local verdict="yes"; [ "$bs" = "$gs" ] && [ "$be" = "$ge" ] || verdict="NO"
  total=$((total+1)); [ "$verdict" = "yes" ] && same=$((same+1))
  printf '%-34s %-9s %-9s %s\n' "$title" "exit $be" "exit $ge" "$verdict"
  if [ "$verdict" = "NO" ]; then echo "    bash: $bs"; echo "    go:   $gs"; fi
}
scenario "HTTP 200"                       --host 127.0.0.1 --port "$HP" --url "http://127.0.0.1:$HP/ok" --skip-tls
scenario "HTTP 404 (expected 200)"        --host 127.0.0.1 --port "$HP" --url "http://127.0.0.1:$HP/missing" --skip-tls
scenario "HTTP 404 (expected 404)"        --host 127.0.0.1 --port "$HP" --url "http://127.0.0.1:$HP/missing" --expect-status 404 --skip-tls
scenario "HTTP 302 redirect"              --host 127.0.0.1 --port "$HP" --url "http://127.0.0.1:$HP/redirect" --skip-tls
scenario "closed TCP port"                --host 127.0.0.1 --port "$P_CLOSED" --skip-tls
scenario "TLS cert valid 365d"            --host 127.0.0.1 --port "$P_OK"
scenario "TLS cert expiring in 20d"       --host 127.0.0.1 --port "$P_WARN"
scenario "TLS cert expiring in 3d"        --host 127.0.0.1 --port "$P_CRIT"
scenario "TLS cert already expired"       --host 127.0.0.1 --port "$P_EXP"
scenario "TLS on a plain HTTP port"       --host 127.0.0.1 --port "$HP"
scenario "name that does not resolve"     --host no-such-name.invalid --port 443 --url https://no-such-name.invalid
scenario "real site (example.com)"        --host example.com --port 443 --url https://example.com
echo; echo "$same of $total scenarios identical (exit code and every per-check status)."

echo; echo "JSON shape (HTTP 200 scenario):"
"$BASH_BIN" --host 127.0.0.1 --port "$HP" --url "http://127.0.0.1:$HP/ok" --skip-tls --json | sed 's/^/  bash: /'
"$GO_BIN"   --host 127.0.0.1 --port "$HP" --url "http://127.0.0.1:$HP/ok" --skip-tls --json | sed 's/^/  go:   /'
