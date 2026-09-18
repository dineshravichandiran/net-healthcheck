#!/usr/bin/env bash
# Shared helpers for net-healthcheck. Bash 3.2+ (macOS-shipped bash), no arrays
# with associative keys, no [[ =~ ]] extensions that differ across bash builds.

# Status ranking, worst wins: OK < WARNING < CRITICAL < UNKNOWN is NOT the
# Nagios order for "worst" — UNKNOWN is worse than OK but a CRITICAL always
# wins over everything. We rank: OK=0, WARNING=1, UNKNOWN=2, CRITICAL=3, and
# take the max, then map back. This matches Nagios convention where a plugin
# reports the single worst condition it found.
STATUS_OK=0
STATUS_WARNING=1
STATUS_CRITICAL=2
STATUS_UNKNOWN=3

_worst_rank() {
  # Returns a sortable rank so CRITICAL always beats WARNING beats UNKNOWN beats OK.
  case "$1" in
    0) echo 0 ;;  # OK
    1) echo 2 ;;  # WARNING
    2) echo 3 ;;  # CRITICAL
    3) echo 1 ;;  # UNKNOWN
    *) echo 3 ;;
  esac
}

NHC_WORST=$STATUS_OK
NHC_JSON_RESULTS=""
NHC_JSON_MODE=0

update_worst() {
  local code="$1"
  local cur_rank next_rank
  cur_rank=$(_worst_rank "$NHC_WORST")
  next_rank=$(_worst_rank "$code")
  if [ "$next_rank" -gt "$cur_rank" ]; then
    NHC_WORST=$code
  fi
}

status_label() {
  case "$1" in
    0) echo "OK" ;;
    1) echo "WARNING" ;;
    2) echo "CRITICAL" ;;
    3) echo "UNKNOWN" ;;
    *) echo "UNKNOWN" ;;
  esac
}

# report <status_code> <check_name> <message>
report() {
  local code="$1" name="$2" msg="$3"
  local label
  label=$(status_label "$code")
  update_worst "$code"

  if [ "$NHC_JSON_MODE" = "1" ]; then
    local esc_name esc_msg
    esc_name=$(json_escape "$name")
    esc_msg=$(json_escape "$msg")
    if [ -n "$NHC_JSON_RESULTS" ]; then
      NHC_JSON_RESULTS="${NHC_JSON_RESULTS},"
    fi
    NHC_JSON_RESULTS="${NHC_JSON_RESULTS}{\"check\":\"${esc_name}\",\"status\":\"${label}\",\"message\":\"${esc_msg}\"}"
  else
    printf '[%-8s] %-22s %s\n' "$label" "$name" "$msg"
  fi
}

json_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

emit_json_summary() {
  local worst_label
  worst_label=$(status_label "$NHC_WORST")
  printf '{"worst_status":"%s","checks":[%s]}\n' "$worst_label" "$NHC_JSON_RESULTS"
}

have_cmd() {
  command -v "$1" >/dev/null 2>&1
}
