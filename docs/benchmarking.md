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
- Docker Desktop on macOS adds a virtual-machine layer; these runs do not use Docker.
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
on the same data.

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

`eval fields` plants a distinct canary email in each of 20 request fields,
sends the request, and checks whether the canary is in the body the upstream
received: `masked`, `LEAKED`, or `rejected` (the proxy refused, which counts as
safe). A **no-proxy control** must show all 20 leaking, otherwise the check could
not tell a masker from a no-op. Run several proxies and `eval merge` their JSON
into one table.

## Adding a rival proxy

Point the rival's upstream at the mock, then:

```
RIVALS="name=http://host:port/v1/chat/completions" scripts/eval.sh
./bin/loadgen -label name -url http://host:port/v1/chat/completions ...
```

`bench/rivals/litellm/` has an **untested** template for LiteLLM with its
Presidio guardrail. Pin versions and read the project's current docs before
publishing a comparison, and publish the rival's configuration next to its numbers:
rivals ship different detectors, so overhead is reported "as shipped", not as a
detection head-to-head.
