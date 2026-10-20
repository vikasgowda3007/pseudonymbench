#!/usr/bin/env bash
# Run this proxy (in a container) and LiteLLM + Presidio through the same
# harness: detection quality, field coverage, and overhead at a modest rate.
#
# Needs Docker. Start the stack first:
#   export PSEUDONYMBENCH_KEY=$(openssl rand -hex 32)
#   docker compose -f bench/rivals/litellm/docker-compose.yml up -d --build
#   scripts/rivals.sh
#
# Both proxies run in containers behind Docker Desktop's network, with the mock
# upstream on the host, so they pay the same extra hop. The rate defaults to 10
# requests/s because the stock Presidio analyzer runs one worker.
set -euo pipefail
cd "$(dirname "$0")/.."

RUN=${RUN:-$(date +%Y%m%d-%H%M%S)}
RATE=${RATE:-10}
DURATION=${DURATION:-40s}
WARMUP=${WARMUP:-5s}
PAYLOADS=${PAYLOADS:-"pii pii-long"}
N=${N:-1000}
DATA=${DATA:-}          # labelled JSONL for the detection run; default: synthetic, N samples
MODE=${MODE:-all}       # eval, bench, lowrate or all
LOW_RATE=${LOW_RATE:-2} # lowrate mode: requests/s, kept under what one analyzer worker can take
LOW_DURATION=${LOW_DURATION:-60s}
MOCK_PORT=19000
NOTE=${NOTE:-"unspecified machine"}
PB=http://localhost:18083
LL=http://localhost:4000
OUT=results/rivals-$RUN
mkdir -p "$OUT" bin data

go build -o bin/mockupstream ./cmd/mockupstream
go build -o bin/loadgen ./cmd/loadgen
go build -o bin/benchreport ./cmd/benchreport
go build -o bin/eval ./cmd/eval

pids=()
cleanup() { for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT
wait_for() { for _ in $(seq 1 120); do curl -fs "$1" >/dev/null 2>&1 && return 0; sleep 1; done; echo "timed out waiting for $1" >&2; exit 1; }

echo "waiting for the stack"
wait_for "$PB/healthz"
wait_for "http://localhost:5002/health"
wait_for "http://localhost:5001/health"
wait_for "$LL/health/liveliness"
{
  echo "image digests:"
  docker compose -f bench/rivals/litellm/docker-compose.yml images --format json 2>/dev/null || true
  docker image inspect --format '{{index .RepoDigests 0}}' ghcr.io/data-privacy-stack/presidio-analyzer:latest ghcr.io/data-privacy-stack/presidio-anonymizer:latest ghcr.io/berriai/litellm:main-stable 2>/dev/null || true
} > "$OUT/images.txt"

# ---- detection quality and field coverage: zero-delay mock ----------------
pkill -f 'bin/mockupstream' 2>/dev/null || true
./bin/mockupstream -listen ":$MOCK_PORT" -first-token 0 -token-delay 0 -chunks 1 -echo=false >"$OUT/mock-eval.log" 2>&1 &
mpid=$!; pids+=($mpid)
wait_for "http://localhost:$MOCK_PORT/healthz"
if [[ -z "$DATA" ]]; then
  DATA=data/synthetic-1-$N.jsonl
  [[ -f "$DATA" ]] || ./bin/eval gen -n "$N" -seed 1 -out "$DATA"
fi
want() { [[ $MODE == all || $MODE == "$1" ]]; }
if want eval; then
{
  echo "# Rivals: detection quality ($DATA)"; echo
  for t in "pseudonymbench-container|$PB" "litellm-presidio|$LL"; do
    label=${t%%|*}; url=${t#*|}
    echo "## $label"
    ./bin/eval proxy -data "$DATA" -target "$url/v1/chat/completions" -mock "http://localhost:$MOCK_PORT" \
      -label "$label" -sequential -H "Authorization: Bearer sk-bench" -out "$OUT/proxy-$label.json"
  done
  echo "# Rivals: request-field coverage"; echo
  fields=()
  for t in "pseudonymbench-container|$PB" "litellm-presidio|$LL"; do
    label=${t%%|*}; url=${t#*|}
    ./bin/eval fields -target "$url/v1/chat/completions" -mock "http://localhost:$MOCK_PORT" -label "$label" -sequential \
      -H "Authorization: Bearer sk-bench" -out "$OUT/fields-$label.json" >/dev/null
    fields+=("$OUT/fields-$label.json")
  done
  ./bin/eval merge "${fields[@]}"
} | tee "$OUT/EVAL.md"
fi
kill $mpid 2>/dev/null || true; sleep 1
want bench || want lowrate || exit 0

# ---- overhead: timed mock -------------------------------------------------
./bin/mockupstream -listen ":$MOCK_PORT" -first-token 150ms -token-delay 12ms -chunks 240 -chunk-bytes 4 >"$OUT/mock.log" 2>&1 &
pids+=($!)
wait_for "http://localhost:$MOCK_PORT/healthz"
run() { # label url payload stream
  local label=$1 url=$2 payload=$3 stream=$4 mode=json flags=()
  [[ $stream == true ]] && { mode=sse; flags+=(-stream); }
  ./bin/loadgen -label "$label" -url "$url/v1/chat/completions" -rate "$RATE" -duration "$DURATION" -warmup "$WARMUP" \
    -payload "$payload" -note "$NOTE" -out "$OUT/$label-$payload-$mode.json" ${flags[@]+"${flags[@]}"} 2>/dev/null \
    || echo "  (flagged invalid or failed: $label $payload $mode)"
}
if want bench; then
for payload in $PAYLOADS; do
  for stream in false true; do
    run direct                 "http://localhost:$MOCK_PORT" "$payload" "$stream"
    run pseudonymbench-container "$PB" "$payload" "$stream"
    run litellm-presidio       "$LL" "$payload" "$stream"
    run direct-repeat          "http://localhost:$MOCK_PORT" "$payload" "$stream"
  done
done
fi

# ---- streaming only, at a rate one analyzer worker can keep up with ---------
# Separates LiteLLM's buffering of the stream from the analyzer saturating.
if want lowrate; then
  LOW=$OUT; [[ $MODE == all ]] && LOW="$OUT/lowrate"; mkdir -p "$LOW"
  for payload in $PAYLOADS; do
    for t in "direct|http://localhost:$MOCK_PORT" "pseudonymbench-container|$PB" "litellm-presidio|$LL" "direct-repeat|http://localhost:$MOCK_PORT"; do
      label=${t%%|*}; url=${t#*|}
      ./bin/loadgen -label "$label" -url "$url/v1/chat/completions" -rate "$LOW_RATE" -duration "$LOW_DURATION" -warmup 6s \
        -payload "$payload" -stream -note "$NOTE" -out "$LOW/$label-$payload-sse.json" 2>/dev/null \
        || echo "  (flagged invalid or failed: $label $payload lowrate)"
    done
  done
fi

overflow=$(curl -fs "http://localhost:$MOCK_PORT/_stats" | sed -n 's/.*"overflow":\([0-9]*\).*/\1/p')
[[ "${overflow:-0}" == "0" ]] || echo "WARNING: $overflow mock replies overflowed the fixed length" >&2
if want bench; then
  ./bin/benchreport -baseline direct "$OUT"/*-json.json "$OUT"/*-sse.json | tee "$OUT/REPORT.md"
fi
if want lowrate; then
  ./bin/benchreport -baseline direct "$LOW"/*-sse.json | tee "$LOW/REPORT.md"
fi
if want bench; then echo; echo "machine: $NOTE" | tee -a "$OUT/REPORT.md"; fi
if want lowrate; then echo "machine: $NOTE" | tee -a "$LOW/REPORT.md"; fi
