#!/usr/bin/env bash
# Run the overhead benchmark matrix: every payload set, JSON and SSE, straight
# to the mock upstream (baseline) and through pseudonymbench.
#
#   scripts/bench.sh                     # defaults below
#   RATE=200 DURATION=60s scripts/bench.sh
#   REDIS_URL=redis://localhost:6379/0 scripts/bench.sh   # adds Redis-vault rows (HSETNX and Lua)
#
# Results go to results/<RUN>/ as JSON plus REPORT.md. Numbers from a laptop
# are for checking the harness, not for publishing: see the README.
set -euo pipefail
cd "$(dirname "$0")/.."

RATE=${RATE:-100}
DURATION=${DURATION:-30s}
WARMUP=${WARMUP:-5s}
PAYLOADS=${PAYLOADS:-"plain pii pii-long tools"}
RUN=${RUN:-$(date +%Y%m%d-%H%M%S)}
NOTE=${NOTE:-"unspecified machine"}
MOCK_PORT=${MOCK_PORT:-19000}
PROXY_PORT=${PROXY_PORT:-18080}
REDIS_PROXY_PORT=${REDIS_PROXY_PORT:-18081}
REDIS_LUA_PROXY_PORT=${REDIS_LUA_PROXY_PORT:-18082}
# Mock model timing: 150 ms prefill, 12 ms per 4-byte chunk, 240 chunks. The reply is
# padded to a fixed 960 bytes so it cannot differ between direct and proxied runs;
# the mock counts replies whose echo overflowed that, and this script checks it.
MOCK_ARGS=${MOCK_ARGS:-"-first-token 150ms -token-delay 12ms -chunks 240 -chunk-bytes 4"}

OUT=results/$RUN
mkdir -p "$OUT" bin
go build -o bin/pseudonymbench ./cmd/pseudonymbench
go build -o bin/mockupstream ./cmd/mockupstream
go build -o bin/loadgen ./cmd/loadgen
go build -o bin/benchreport ./cmd/benchreport

pids=()
cleanup() { for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

wait_for() { # url
  for _ in $(seq 1 50); do curl -fs "$1" >/dev/null 2>&1 && return 0; sleep 0.1; done
  echo "timed out waiting for $1" >&2; exit 1
}

# shellcheck disable=SC2086
./bin/mockupstream -listen ":$MOCK_PORT" $MOCK_ARGS >"$OUT/mock.log" 2>&1 &
pids+=($!)
export PSEUDONYMBENCH_KEY=${PSEUDONYMBENCH_KEY:-$(openssl rand -hex 32)}
./bin/pseudonymbench -listen ":$PROXY_PORT" -upstream "http://localhost:$MOCK_PORT" -audit off >"$OUT/proxy-memory.log" 2>&1 &
pids+=($!)
wait_for "http://localhost:$MOCK_PORT/healthz"
wait_for "http://localhost:$PROXY_PORT/healthz"
if [[ -n "${REDIS_URL:-}" ]]; then
  ./bin/pseudonymbench -listen ":$REDIS_PROXY_PORT" -upstream "http://localhost:$MOCK_PORT" -audit off \
    -vault redis -redis-url "$REDIS_URL" -redis-lua=false >"$OUT/proxy-redis.log" 2>&1 &
  pids+=($!)
  wait_for "http://localhost:$REDIS_PROXY_PORT/healthz"
  ./bin/pseudonymbench -listen ":$REDIS_LUA_PROXY_PORT" -upstream "http://localhost:$MOCK_PORT" -audit off \
    -vault redis -redis-url "$REDIS_URL" -redis-lua=true >"$OUT/proxy-redis-lua.log" 2>&1 &
  pids+=($!)
  wait_for "http://localhost:$REDIS_LUA_PROXY_PORT/healthz"
fi

run() { # label url payload stream
  local label=$1 url=$2 payload=$3 stream=$4 mode=json flags=()
  [[ $stream == true ]] && { mode=sse; flags+=(-stream); }
  ./bin/loadgen -label "$label" -url "$url/v1/chat/completions" -rate "$RATE" -duration "$DURATION" \
    -warmup "$WARMUP" -payload "$payload" -note "$NOTE" -out "$OUT/$label-$payload-$mode.json" ${flags[@]+"${flags[@]}"} \
    2>/dev/null || echo "  (run flagged invalid or failed: $label $payload $mode)"
}

for payload in $PAYLOADS; do
  for stream in false true; do
    run direct          "http://localhost:$MOCK_PORT"  "$payload" "$stream"
    run pseudonymbench  "http://localhost:$PROXY_PORT" "$payload" "$stream"
    if [[ -n "${REDIS_URL:-}" ]]; then
      run pseudonymbench-redis-hsetnx "http://localhost:$REDIS_PROXY_PORT"      "$payload" "$stream"
      run pseudonymbench-redis-lua "http://localhost:$REDIS_LUA_PROXY_PORT"  "$payload" "$stream"
    fi
    # A second baseline run: its delta against the first is the noise floor.
    run direct-repeat   "http://localhost:$MOCK_PORT"  "$payload" "$stream"
  done
done

overflow=$(curl -fs "http://localhost:$MOCK_PORT/_stats" | sed -n 's/.*"overflow":\([0-9]*\).*/\1/p')
if [[ "${overflow:-0}" != "0" ]]; then
  echo "WARNING: $overflow mock replies overflowed the fixed length; raise -chunks in MOCK_ARGS or the comparison is biased" >&2
fi
./bin/benchreport -baseline direct "$OUT"/*.json | tee "$OUT/REPORT.md"
echo
echo "machine: $NOTE" | tee -a "$OUT/REPORT.md"
