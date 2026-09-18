#!/usr/bin/env bash
# OSI-layer network checks: DNS, TCP reachability, HTTP(S), TLS cert expiry.
# Every check degrades to UNKNOWN when its underlying tool is missing rather
# than silently skipping or crashing — same convention as the rest of the
# ops-toolkits family.

# _with_timeout <seconds> <cmd...>
# Portable timeout: macOS bash 3.2 has no `timeout` binary (that's GNU
# coreutils only), so run the command in the background and race it against
# a sleep in the foreground.
_with_timeout() {
  local secs="$1"
  shift
  local out rc
  local tmp
  tmp=$(mktemp)
  ("$@" >"$tmp" 2>&1; echo $? >"${tmp}.rc") &
  local pid=$!
  local waited=0
  while kill -0 "$pid" 2>/dev/null; do
    sleep 0.2
    waited=$(awk -v w="$waited" 'BEGIN{printf "%.1f", w+0.2}')
    if awk -v w="$waited" -v s="$secs" 'BEGIN{exit !(w>=s)}'; then
      kill -9 "$pid" 2>/dev/null
      wait "$pid" 2>/dev/null
      rm -f "$tmp" "${tmp}.rc"
      return 124
    fi
  done
  wait "$pid" 2>/dev/null
  out=$(cat "$tmp" 2>/dev/null)
  rc=$(cat "${tmp}.rc" 2>/dev/null)
  rm -f "$tmp" "${tmp}.rc"
  printf '%s' "$out"
  return "${rc:-1}"
}

# is_ip_literal <string> -- true if already an IPv4 or IPv6 address, not a
# name to resolve. Found this the hard way: `dig +short 127.0.0.1` queries
# 127.0.0.1 as if it were a hostname (there's no implicit reverse lookup)
# and returns nothing, so DNS-checking a target that's already an IP was
# reporting a false CRITICAL "did not resolve" on every run.
is_ip_literal() {
  case "$1" in
    *:*) return 0 ;;  # crude but sufficient IPv6 literal check for our purposes
  esac
  echo "$1" | grep -Eq '^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$'
}

# check_dns <hostname>
check_dns() {
  local host="$1"
  local resolved="" tool=""

  if is_ip_literal "$host"; then
    report "$STATUS_OK" "dns:$host" "already an IP literal, nothing to resolve"
    return
  fi

  if have_cmd dig; then
    tool="dig"
    resolved=$(dig +short +time=3 +tries=1 "$host" 2>/dev/null | grep -v '\.$' | head -1)
    if [ -z "$resolved" ]; then
      resolved=$(dig +short +time=3 +tries=1 "$host" 2>/dev/null | head -1)
    fi
  elif have_cmd host; then
    tool="host"
    resolved=$(host -W 3 "$host" 2>/dev/null | awk '/has address/ {print $NF; exit}')
  elif have_cmd python3; then
    tool="python3 socket"
    resolved=$(python3 -c "import socket,sys
try:
    print(socket.gethostbyname(sys.argv[1]))
except Exception:
    pass" "$host" 2>/dev/null)
  fi

  if [ -z "$tool" ]; then
    report "$STATUS_UNKNOWN" "dns:$host" "no DNS resolver available (need dig, host, or python3)"
    return
  fi

  if [ -n "$resolved" ]; then
    report "$STATUS_OK" "dns:$host" "resolved to $resolved (via $tool)"
  else
    report "$STATUS_CRITICAL" "dns:$host" "did not resolve (via $tool)"
  fi
}

# check_tcp <hostname> <port> [timeout_seconds]
#
# Deliberately does NOT use bash's `/dev/tcp/host/port` redirection. That
# only works on a bash built with --enable-net-redirections, and Apple's
# shipped /bin/bash (3.2.57) is compiled WITHOUT it — `/dev/tcp/...` fails
# with a plain "no such file or directory" on real macOS, silently, with no
# hint that it's a compile-time flag rather than a real filesystem path.
# Found by running this against a known-reachable host and getting a false
# CRITICAL. nc (BSD netcat, ships with macOS) and a python3 socket fallback
# both work correctly on the same host.
check_tcp() {
  local host="$1" port="$2" timeout="${3:-3}"
  local start end elapsed_ms tool ok=1

  start=$(python3 -c 'import time; print(time.time())' 2>/dev/null || echo 0)

  if have_cmd nc; then
    tool="nc"
    if nc -z -w "$timeout" "$host" "$port" >/dev/null 2>&1; then
      ok=0
    fi
  elif have_cmd python3; then
    tool="python3 socket"
    if python3 -c "
import socket, sys
s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
s.settimeout(float(sys.argv[2]))
try:
    s.connect((sys.argv[0], int(sys.argv[1])))
    sys.exit(0)
except Exception:
    sys.exit(1)
" "$host" "$port" "$timeout" >/dev/null 2>&1; then
      ok=0
    fi
  else
    report "$STATUS_UNKNOWN" "tcp:$host:$port" "no TCP-check tool available (need nc or python3)"
    return
  fi

  end=$(python3 -c 'import time; print(time.time())' 2>/dev/null || echo 0)
  elapsed_ms=$(awk -v s="$start" -v e="$end" 'BEGIN{printf "%.0f", (e-s)*1000}')

  if [ "$ok" = "0" ]; then
    report "$STATUS_OK" "tcp:$host:$port" "connected in ${elapsed_ms}ms (via $tool)"
  else
    report "$STATUS_CRITICAL" "tcp:$host:$port" "connection refused or timed out (via $tool)"
  fi
}

# check_http <url> [expected_status]
check_http() {
  local url="$1" expected="${2:-200}"
  if ! have_cmd curl; then
    report "$STATUS_UNKNOWN" "http:$url" "curl not available"
    return
  fi

  local result code time_total
  result=$(curl -k -s -o /dev/null -m 5 -w '%{http_code} %{time_total}' "$url" 2>/dev/null)
  local curl_rc=$?
  if [ "$curl_rc" -ne 0 ] || [ -z "$result" ]; then
    report "$STATUS_CRITICAL" "http:$url" "request failed (curl exit $curl_rc)"
    return
  fi

  code=$(echo "$result" | awk '{print $1}')
  time_total=$(echo "$result" | awk '{print $2}')

  if [ "$code" = "$expected" ]; then
    report "$STATUS_OK" "http:$url" "HTTP $code in ${time_total}s"
  elif [ "${code:0:1}" = "2" ] || [ "${code:0:1}" = "3" ]; then
    report "$STATUS_WARNING" "http:$url" "HTTP $code (expected $expected) in ${time_total}s"
  else
    report "$STATUS_CRITICAL" "http:$url" "HTTP $code (expected $expected) in ${time_total}s"
  fi
}

# check_tls_expiry <hostname> <port> [warn_days] [crit_days]
check_tls_expiry() {
  local host="$1" port="$2" warn_days="${3:-30}" crit_days="${4:-7}"
  if ! have_cmd openssl; then
    report "$STATUS_UNKNOWN" "tls:$host:$port" "openssl not available"
    return
  fi

  local enddate epoch_end epoch_now days_left
  enddate=$(_with_timeout 5 bash -c "echo | openssl s_client -connect '${host}:${port}' -servername '${host}' 2>/dev/null | openssl x509 -noout -enddate 2>/dev/null" | sed 's/^notAfter=//')

  if [ -z "$enddate" ]; then
    report "$STATUS_CRITICAL" "tls:$host:$port" "could not retrieve certificate (handshake failed or timed out)"
    return
  fi

  epoch_end=$(date -j -f "%b %d %T %Y %Z" "$enddate" "+%s" 2>/dev/null)
  if [ -z "$epoch_end" ]; then
    # GNU date fallback (Linux)
    epoch_end=$(date -d "$enddate" "+%s" 2>/dev/null)
  fi
  if [ -z "$epoch_end" ]; then
    report "$STATUS_UNKNOWN" "tls:$host:$port" "certificate found but could not parse expiry '$enddate'"
    return
  fi

  epoch_now=$(date "+%s")
  days_left=$(awk -v e="$epoch_end" -v n="$epoch_now" 'BEGIN{printf "%.0f", (e-n)/86400}')

  if [ "$days_left" -lt 0 ]; then
    report "$STATUS_CRITICAL" "tls:$host:$port" "certificate EXPIRED $((days_left * -1)) day(s) ago ($enddate)"
  elif [ "$days_left" -lt "$crit_days" ]; then
    report "$STATUS_CRITICAL" "tls:$host:$port" "certificate expires in ${days_left}d ($enddate)"
  elif [ "$days_left" -lt "$warn_days" ]; then
    report "$STATUS_WARNING" "tls:$host:$port" "certificate expires in ${days_left}d ($enddate)"
  else
    report "$STATUS_OK" "tls:$host:$port" "certificate valid for ${days_left}d ($enddate)"
  fi
}
