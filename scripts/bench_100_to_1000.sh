#!/usr/bin/env bash
set -euo pipefail
BASE=${BASE:-http://127.0.0.1:8080}
URL=${URL:-"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}
QUALITY=${QUALITY:-128}

run() {
  local N=$1
  echo "== Running bench for N=$N =="
  /workspace/bin/bench -base "$BASE" -url "$URL" -n "$N" -q "$QUALITY" -delay 200ms | tee "/tmp/bench_${N}.txt"
}

for N in 100 200 500 1000; do
  run "$N"
  echo
  echo "Last 20 lines of bench_${N}.txt:" 
  tail -n 20 "/tmp/bench_${N}.txt" || true
  echo
 done
