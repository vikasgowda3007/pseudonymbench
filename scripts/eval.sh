#!/usr/bin/env bash
# Score masking quality: per-entity precision/recall (detector alone and through
# the proxy) and the request-field coverage matrix.
#
#   scripts/eval.sh                     # synthetic data, this proxy only
#   DATA=data/other.jsonl scripts/eval.sh
#
# Add rival proxies by listing "label=url" pairs in RIVALS, where url is the
# rival's chat-completions endpoint and its upstream is already pointed at the
# mock (default http://localhost:19001):
#   RIVALS="litellm=http://localhost:4000/v1/chat/completions" scripts/eval.sh
set -euo pipefail
cd "$(dirname "$0")/.."

RUN=${RUN:-$(date +%Y%m%d-%H%M%S)}
N=${N:-2000}
SEED=${SEED:-1}
DATA=${DATA:-}
MOCK_PORT=${MOCK_PORT:-19001}
PROXY_PORT=${PROXY_PORT:-18090}
RIVALS=${RIVALS:-}
OUT=results/eval-$RUN
mkdir -p "$OUT" bin data

go build -o bin/pseudonymbench ./cmd/pseudonymbench
go build -o bin/mockupstream ./cmd/mockupstream
go build -o bin/eval ./cmd/eval

if [[ -z "$DATA" ]]; then
  DATA=data/synthetic-$SEED-$N.jsonl
  ./bin/eval gen -n "$N" -seed "$SEED" -out "$DATA"
fi

pids=()
trap 'for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done' EXIT
wait_for() { for _ in $(seq 1 50); do curl -fs "$1" >/dev/null 2>&1 && return 0; sleep 0.1; done; echo "timed out: $1" >&2; exit 1; }

# No simulated model time: evaluation only needs the recorded request bodies.
./bin/mockupstream -listen ":$MOCK_PORT" -first-token 0 -token-delay 0 -chunks 1 -echo=false >"$OUT/mock.log" 2>&1 &
pids+=($!)
PSEUDONYMBENCH_KEY=$(openssl rand -hex 32) ./bin/pseudonymbench -listen ":$PROXY_PORT" \
  -upstream "http://localhost:$MOCK_PORT" -audit off >"$OUT/proxy.log" 2>&1 &
pids+=($!)
wait_for "http://localhost:$MOCK_PORT/healthz"
wait_for "http://localhost:$PROXY_PORT/healthz"

MOCK=http://localhost:$MOCK_PORT
{
  echo "# Evaluation $RUN"
  echo
  echo "Data: \`$DATA\` ($(wc -l <"$DATA" | tr -d ' ') samples)."
  echo
  echo "## Detector alone"
  ./bin/eval detector -data "$DATA" -out "$OUT/detector.json"
  echo "## Through the proxy"
  ./bin/eval proxy -data "$DATA" -target "http://localhost:$PROXY_PORT/v1/chat/completions" -mock "$MOCK" \
    -label pseudonymbench -out "$OUT/proxy-pseudonymbench.json"
  for pair in $RIVALS; do
    label=${pair%%=*}; url=${pair#*=}
    echo "## Through $label"
    ./bin/eval proxy -data "$DATA" -target "$url" -mock "$MOCK" -label "$label" -out "$OUT/proxy-$label.json"
  done
} | tee "$OUT/EVAL.md"

{
  echo
  echo "## Request-field coverage"
  echo
  ./bin/eval fields -target "http://localhost:$PROXY_PORT/v1/chat/completions" -mock "$MOCK" -label pseudonymbench \
    -out "$OUT/fields-pseudonymbench.json" >/dev/null
  fields=("$OUT/fields-pseudonymbench.json")
  for pair in $RIVALS; do
    label=${pair%%=*}; url=${pair#*=}
    ./bin/eval fields -target "$url" -mock "$MOCK" -label "$label" -out "$OUT/fields-$label.json" >/dev/null
    fields+=("$OUT/fields-$label.json")
  done
  ./bin/eval merge "${fields[@]}"
} | tee -a "$OUT/EVAL.md"
