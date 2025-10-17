#!/usr/bin/env bash
set -euo pipefail

BASE=${BASE:-http://127.0.0.1:8080}
URL=${1:-"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}
MODE=${2:-immediate}   # immediate | after_download
QUALITY=${3:-128}
RANGE_SPEC=${4:-}      # optional: 00:01:00-00:02:00

json_field() {
  # $1 field, read stdin JSON, naive sed/grep
  sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p"
}

now_ms() { date +%s%3N; }
elapsed() { # ms -> s.mmm
  awk -v ms="$1" 'BEGIN{printf("%.3f", ms/1000)}'
}

log() {
  printf "%s %s\n" "[$(date +%T)]" "$*"
}

# 1) PREPARE
start_ms=$(now_ms)
prep_json=$(curl -sS -X POST "$BASE/prepare" -H 'Content-Type: application/json' -d "{\"url\":\"$URL\"}") || { echo "prepare failed"; exit 1; }
prep_end_ms=$(now_ms)
CID=$(printf '%s' "$prep_json" | json_field conversion_id)
[ -n "$CID" ] || { echo "missing conversion_id"; echo "$prep_json"; exit 1; }
meta_ms=$((prep_end_ms - start_ms))
log "prepare OK id=$CID meta_latency=$(elapsed $meta_ms)s"

# 2) CONVERT (immediate or after_download)
convert_req_ms=0
if [ "$MODE" = "immediate" ]; then
  body="{\"conversion_id\":\"$CID\",\"quality\":\"$QUALITY\"}"
  if [ -n "$RANGE_SPEC" ]; then
    s=${RANGE_SPEC%-*}; e=${RANGE_SPEC#*-}
    body="{\"conversion_id\":\"$CID\",\"quality\":\"$QUALITY\",\"start_time\":\"$s\",\"end_time\":\"$e\"}"
  fi
  curl -sS -X POST "$BASE/convert" -H 'Content-Type: application/json' -d "$body" >/dev/null || true
  convert_req_ms=$(now_ms)
  log "convert queued (immediate)"
else
  log "waiting for downloaded state before queuing convert..."
fi

# 3) POLL STATUS EACH SECOND UNTIL COMPLETED
status_url="$BASE/status/$CID"
convert_start_ms=0
download_start_ms=0
download_end_ms=0
completed_ms=0

while :; do
  sleep 1
  st_json=$(curl -sS "$status_url" || true)
  st=$(printf '%s' "$st_json" | json_field status)
  dl=$(printf '%s' "$st_json" | sed -n 's/.*"download_progress":\s*\([0-9]\+\).*/\1/p')
  cv=$(printf '%s' "$st_json" | sed -n 's/.*"conversion_progress":\s*\([0-9]\+\).*/\1/p')
  qp=$(printf '%s' "$st_json" | sed -n 's/.*"queue_position":\s*\([0-9]\+\).*/\1/p')
  now=$(now_ms)
  printf "t=+%ss status=%s dl=%s%% cv=%s%% qpos=%s\n" "$(elapsed $((now-start_ms)))" "$st" "$dl" "$cv" "${qp:-0}"

  case "$st" in
    downloading)
      if [ $download_start_ms -eq 0 ]; then download_start_ms=$now; fi ;;
    downloaded)
      if [ $download_end_ms -eq 0 ]; then download_end_ms=$now; fi
      if [ "$MODE" = "after_download" ] && [ $convert_req_ms -eq 0 ]; then
        body="{\"conversion_id\":\"$CID\",\"quality\":\"$QUALITY\"}"
        if [ -n "$RANGE_SPEC" ]; then s=${RANGE_SPEC%-*}; e=${RANGE_SPEC#*-}; body="{\"conversion_id\":\"$CID\",\"quality\":\"$QUALITY\",\"start_time\":\"$s\",\"end_time\":\"$e\"}"; fi
        curl -sS -X POST "$BASE/convert" -H 'Content-Type: application/json' -d "$body" >/dev/null || true
        convert_req_ms=$(now_ms)
        log "convert queued (after_download)"
      fi ;;
    converting)
      if [ $convert_start_ms -eq 0 ]; then
        convert_start_ms=$now
        if [ $download_end_ms -eq 0 ] && [ "$dl" = "100" ]; then download_end_ms=$now; fi
      fi ;;
    completed)
      completed_ms=$now; break ;;
    failed)
      log "failed: $st_json"; exit 1 ;;
  esac

done

# 4) REPORT DURATIONS
queue_wait_ms=0
convert_ms=0
download_ms=0
[ $convert_req_ms -gt 0 ] && [ $convert_start_ms -gt 0 ] && queue_wait_ms=$((convert_start_ms - convert_req_ms))
[ $download_end_ms -gt 0 ] && download_ms=$((download_end_ms - start_ms))
[ $convert_start_ms -gt 0 ] && [ $completed_ms -gt 0 ] && convert_ms=$((completed_ms - convert_start_ms))

echo
log "REPORT ($MODE, quality=$QUALITY)"
log "meta_latency=$(elapsed $meta_ms)s"
log "download_duration=$(elapsed $download_ms)s"
log "queue_wait=$(elapsed $queue_wait_ms)s"
log "convert_duration=$(elapsed $convert_ms)s"
log "end_to_end=$(elapsed $((completed_ms - start_ms)))s"
