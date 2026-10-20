# Benchmarking method

Two questions, answered separately:

1. **What does the proxy cost?** Added latency and added time-to-first-token (TTFT) at a fixed request rate, against going straight to the upstream.
2. **What does it catch?** Per-entity precision and recall, and which request fields leak.

Everything is reproducible from this repo with two scripts. Results from one
machine are a record of that machine, not a spec.

## 1. Overhead

```
scripts/bench.sh                              # defaults: 100 rps, 30 s + 5 s warmup
RATE=200 DURATION=60s scripts/bench.sh
REDIS_URL=redis://localhost:6379/0 scripts/bench.sh   # adds a Redis-vault row
```

### Setup

- **Upstream is a mock** (`cmd/mockupstream`), not a real model. A real API's
  variance would swamp a proxy that adds a millisecond. The mock replies after a
  fixed prefill delay (150 ms) and then one 4-byte chunk every 12 ms (240
  chunks), the shape of a streamed completion.
- **The reply has a fixed length.** The mock pads every reply to the same number
  of bytes (960), whatever the request said, and counts replies whose echo was too
  long to fit (`/_stats` → `overflow`; `scripts/bench.sh` warns if it is not 0). Otherwise a masked prompt (shorter or
  longer than the original) would change how long the mock "generates" and
  pollute the comparison. The reply echoes every PII value or token found in the
  request, so the proxy has tokens to restore.
- **Baseline** is the same load straight at the mock. The reported overhead is
  `proxy − baseline` at the same payload, mode and rate, for each percentile.
- **Noise floor.** A second baseline run (`direct-repeat`) is reported against the
  first. Differences between proxy and baseline smaller than the
  `direct-repeat` difference are not distinguishable from noise.

### The load generator is open-loop

`cmd/loadgen` sends request *i* at `start + i / rate` whatever happened to earlier
requests, and measures latency from that **intended** send time. A closed-loop
client waits for a response before sending the next, so when the server stalls
the client quietly stops sending exactly the requests that would have queued: the
coordinated-omission effect described in the wrk2 README and in Gil Tene's "How
NOT to Measure Latency". k6's `constant-arrival-rate` executor is the same idea.

A purpose-built client is used because none of wrk2, vegeta, oha or k6 documents
per-event timing for SSE (k6 would need a third-party extension). The client
reports:

| Field | Meaning |
|---|---|
| total | intended send time to end of response body |
| TTFT (streams) | intended send time to the first SSE event whose delta has non-empty `content` |
| TTFB | intended send time to the first response byte (headers) |
| schedule lag | how late the generator actually sent, versus the schedule |
| valid | false if schedule-lag p99 exceeds 5 ms or requests were dropped: the generator, not the proxy, was the bottleneck, so the run must not be quoted |

Percentiles are nearest-rank over the measured window (warmup excluded). With
fewer than 1,000 samples a p99.9 is just the maximum.

### Payload sets

`plain` (no PII), `pii` (one message with an email, phone, card, SSN and IBAN),
`pii-long` (system prompt plus a multi-turn history with about 20 values),
`tools` (PII in a user message, tool-call arguments and a tool result). Values
are regenerated for every request from a seeded generator.

### Reading the report

`benchreport` prints one row per run with Δ columns against the baseline. A
`direct-repeat` row near zero means the machine was quiet enough to trust the
other rows.

### What this does not tell you

- It is one machine, usually a laptop with the generator, proxy and mock sharing
  cores. Publish numbers from a quiet Linux host with pinned CPUs.
- Docker Desktop on macOS adds a virtual-machine layer. The overhead matrix runs natively; the Redis and rival runs use Docker Desktop and say so.
- It does not measure a real model, a real network or TLS.
- The Redis row needs a Redis server; the numbers include a localhost round trip.

## 2. Detection quality

```
scripts/eval.sh                                       # synthetic set, 2,000 samples
DATA=data/nemotron-pii.jsonl scripts/eval.sh          # after: go run ./cmd/dataprep nemotron
RIVALS="litellm=http://localhost:4000/v1/chat/completions" scripts/eval.sh
```

### How it measures, from the outside

For each labelled sample the tool sends the text through the proxy to the
recording mock, reads back what the upstream received, aligns it with the
original (word-level longest common subsequence), and treats each run of changed
words as a **replaced region**, trimming characters the replacement shares with
the original (`john@acme.com.` → `<EMAIL_x>.` reports `john@acme.com`). Because
it never looks inside the proxy, the same measurement applies to any proxy whose
placeholders it can recognise (this tool's tokens and common forms such as
`[EMAIL_ADDRESS]` or `<PHONE_NUMBER>`).

A test checks that this black-box number equals running the detectors directly
on the same data. On the Nemotron sample the overlap recall and precision are
identical through the proxy and direct; the exact-span recall through the proxy is
up to 1.3 points lower (email 0.991 against 0.998, phone 0.984 against 0.997),
because word-level diffing cannot always recover a boundary exactly. The README
quotes overlap recall for that reason.

### Metrics

| Metric | Definition |
|---|---|
| recall (overlap) | share of gold spans touched by a replaced region |
| recall (exact) | share of gold spans whose boundaries a region matched exactly |
| bytes left visible | share of gold-span bytes not covered by any region. A half-masked SSN still leaks digits, so this is stricter than recall |
| precision (typed) | for each placeholder of kind K, whether its region overlaps a gold span of kind K |
| F2 | F-score weighting recall twice as much as precision; masking too much usually costs less than missing PII |
| clean-text rate | share of PII-free samples where anything was replaced |

Gold spans of PII the regex tier is not built for (names, addresses, ...) are
tagged `OTHER`: they are neither hits nor misses, and a replacement over them is
not a false positive.

### Datasets

| Set | Source | Licence | Notes |
|---|---|---|---|
| synthetic | `internal/synth`, seeded | this repo | Values follow the same public rules the detectors check (Luhn, mod-97, SSA structure), so recall here is a self-check. Hard negatives are included so precision is not trivially 1.0 |
| Nemotron-PII | `nvidia/Nemotron-PII`, test split, fetched on demand by `cmd/dataprep` | CC-BY-4.0 | Synthetic, LLM-generated text with span labels. Its card numbers mostly fail Luhn (see the threat model), and it has no IBAN labels |

Other candidates and why they are not used: `ai4privacy/pii-masking-200k/300k/400k`
have licences that forbid redistribution; `gretelai/gretel-pii-masking-en-v1`
(Apache-2.0) would need a converter (its labels carry entity text, not offsets).

## 3. Field coverage

`eval fields` plants a distinct canary email in each of 21 request fields,
sends the request, and checks whether the canary is in the body the upstream
received: `masked`, `LEAKED`, `dropped` (a top-level key that never reached the
upstream) or `rejected` (the proxy refused); the last two count as safe. A **no-proxy control** must show all 21 leaking, otherwise the check could
not tell a masker from a no-op. Run several proxies and `eval merge` their JSON
into one table.

## Adding a rival proxy

Point the rival's upstream at the mock, then:

```
RIVALS="name=http://host:port/v1/chat/completions" scripts/eval.sh
./bin/loadgen -label name -url http://host:port/v1/chat/completions ...
```

`bench/rivals/litellm/` runs LiteLLM with its Presidio guardrail next to this
proxy, both in containers behind Docker Desktop's network, with the mock on the
host:

```
export PSEUDONYMBENCH_KEY=$(openssl rand -hex 32)
docker compose -f bench/rivals/litellm/docker-compose.yml up -d --build
scripts/rivals.sh                                   # detection, field matrix, overhead at 10 req/s
DATA=data/nemotron-pii.jsonl MODE=eval scripts/rivals.sh
```

Things that went wrong or needed care when this was first run, so they do not
catch the next person:

- **Pin the images.** `scripts/rivals.sh` writes the digests it ran to
  `images.txt`. The compose file uses moving tags.
- **The canary must look like an email to the rival's detector.** Presidio
  validates top-level domains, so a `.test` canary made LiteLLM look like it
  leaked fields it had simply not recognised; the canary uses `example.com`.
  A "LEAKED" cell means the canary reached the upstream, which for a
  recogniser-based rival can also mean "not recognised as PII".
- **"masked" is not proof for a proxy that may drop a field.** LiteLLM did not
  forward `user` or `metadata` to the upstream in this configuration, so those
  cells first read "masked". The matrix now reports `dropped` when a top-level key
  the client sent is missing from the upstream body (counted as not leaked, listed
  separately). Fields inside messages cannot be told apart this way.
- **Exact-span scores are meaningless for a rival that rewrites whitespace.**
  LiteLLM removes the space after a placeholder; read overlap recall.
- **The stock Presidio analyzer runs one worker.** At 10 req/s it saturates on
  the longer payload, so the comparison also has a low-rate run that separates
  buffering from saturation (`MODE=lowrate scripts/rivals.sh`).
- **Attribute a rival's behaviour before you describe it.** LiteLLM's buffered stream
  was checked three ways with the mock at 150 ms prefill (3 requests each): no
  guardrail, first byte 0.17 s; guardrail with `output_parse_pii: false`, 3.4 s;
  guardrail with `output_parse_pii: true`, 3.3 to 4.0 s.
- **Rivals ship different detectors**, so overhead is reported "as shipped" and
  detection is not a head-to-head: Presidio masks names, addresses and dates
  that this proxy does not look for.

Publish the rival's configuration next to its numbers (`config.yaml` is the one used).
