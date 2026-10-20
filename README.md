# pseudonymbench

Mask PII before a prompt reaches an LLM API, put it back in the reply, and measure what that costs and what it misses.

pseudonymbench is a small reverse proxy for OpenAI-style chat-completions APIs, plus the benchmark harness used to test it. The proxy replaces emails, phone numbers, SSNs, card numbers and IBANs with random tokens such as `<EMAIL_7f3a9c21>`, keeps the real values in a vault, and restores them in the response, including when a token arrives split across streamed chunks. The harness measures what other tools in this space rarely publish: added latency and time-to-first-token at a fixed arrival rate, per-entity precision and recall, and which request fields leak.

> **Status: research-grade.** Numbers below are from synthetic and public datasets and from one laptop. This is pseudonymization, not anonymization, and it makes no compliance claim. Read [what it misses](#what-it-misses) before trusting it with anything real.

## Results

All of these are reproducible from this repo; commands are under [Reproduce](#reproduce). Method and caveats: [docs/benchmarking.md](docs/benchmarking.md).

### What the proxy costs

Added latency against going straight to the upstream, at 100 requests/s open-loop, 3,000 requests per cell (30 s after a 4 s warmup). The mock upstream waits 150 ms (prefill), then streams a 960-byte reply as 240 chunks 12 ms apart, so a completion takes about 3 s. Apple M4, 16 GB, macOS, Go 1.26.3; generator, proxy and mock share the machine and no run used Docker, so treat this as a laptop result, not a benchmark of record. In-memory vault in the first table; Redis in the second.

<!-- overhead:start -->
| payload | JSON: added p50 (ms) | JSON: added p99 (ms) | stream: added TTFT p50 (ms) | stream: added TTFT p99 (ms) | baseline TTFT p50 / p99 (ms) | noise floor: JSON p50 / p99, TTFT p50 / p99 (ms) |
|---|---:|---:|---:|---:|---:|---|
| pii | +0.5 | +0.6 | +0.5 | -2.5 | 151.1 / 158.5 | 0.2 / 0.1, 0.0 / 4.4 |
| pii-long | +0.7 | +0.9 | +0.6 | +1.5 | 151.2 / 154.3 | 0.1 / 0.2, 0.1 / 0.2 |
| plain | +1.4 | +6.9 | +0.4 | +0.7 | 151.2 / 155.2 | 1.1 / 1.9, 0.0 / 0.1 |
| tools | +0.4 | +0.6 | +0.4 | +1.2 | 151.2 / 154.6 | 0.1 / 0.0, 0.0 / 0.0 |

What this says:

- **The proxy adds about half a millisecond at the median** for payloads that contain PII, both to a complete JSON response and to the time until the first streamed token (baseline first token: 151 ms). At p99 the added time is about 1 ms for those payloads, close to what this machine can resolve: two identical baseline runs differ by up to 4.4 ms at p99 for time to first token (right-hand column), so read p99 as "a millisecond or two", not as a precise number.
- **The `plain` row is worse than the PII rows, which is backwards.** Its median gap (+1.4 ms) is inside the noise of that cell pair (the repeat baseline was 1.1 ms slower than the first). Its p99 gap (+6.9 ms) is larger than that pair's noise floor (1.9 ms), and I have not found a cause: the same cell showed +0.9 ms and +0.8 ms in two earlier full runs, so it looks like a one-off tail event on a shared laptop, but a quiet host and more samples are needed to say.
- **Streams are compared on time to first token only.** The total duration of a 240-chunk stream varies by up to 25 ms between two identical baseline runs on this machine (timer jitter accumulates over 240 sleeps), which is larger than any effect being measured. The full table with those columns is in [`results/laptop-3/REPORT.md`](results/laptop-3/REPORT.md).
- Detection itself is cheap: `go test -bench` measures about 1.4 µs for 2 KB of prose and about 22 µs for 1.8 KB containing 40 PII values. The rest of the half millisecond (the extra network hop, JSON decode and encode, the vault) is not broken down here.


**With Redis as the vault** (Redis 7.4 in Docker Desktop on the same laptop, so each vault round trip also crosses the VM's port forward; proxy, mock and load generator native; 2,500 requests per cell; `results/redis-3/` for `pii` and `results/redis-3-long/` for `pii-long`). Added median in ms, JSON response / time to first token:

| payload | memory vault | Redis, `HSETNX` sequence | Redis, Lua, concurrent calls | Redis, Lua, one pipelined round trip (default) |
|---|---:|---:|---:|---:|
| pii (5 values) | +0.5 / +0.4 | +3.0 / +2.8 | +2.0 / +1.6 | +1.6 / +1.7 |
| pii-long (about 20 values) | +0.8 / +0.5 | +3.4 / +4.1 | +3.4 / +3.4 | +2.6 / +2.0 |

A remote vault costs a millisecond or three on this setup, and it grows with the number of distinct values in a request. The p99 column is not quoted: this laptop was shared with other work, identical cells differed by tens of milliseconds at p99 between passes, and several cells were flagged `valid=false` by the load generator (they are in the committed `REPORT.md` files and not used here; the `pii-long` rows come from a second pass whose own baseline was flagged, so they are measured against that pass's valid repeat run, `REPORT-baseline-repeat.md`). Against `HSETNX`, Lua is faster everywhere; the pipelined batch beats concurrent single calls by about a millisecond on the 20-value payload and is within noise on the 5-value one. The three Redis modes are measured in isolation, where the noise is much lower, by `go test` against the same Redis (masking one request including the cleanup round trip, `BenchmarkRequestRedis`):

| payload | `HSETNX` sequence | Lua, concurrent calls | Lua, pipelined (default) |
|---|---:|---:|---:|
| pii (5 values) | 0.71 to 0.80 ms | 0.37 to 0.48 ms | 0.25 to 0.29 ms |
| pii-long (about 20 values) | 2.2 to 2.3 ms | 1.0 to 1.25 ms | 0.48 ms |

(three runs each; `go test ./internal/mask -run xxx -bench RequestRedis` with `PSEUDONYMBENCH_TEST_REDIS=localhost:6379`.) The pipelined path sends one `EVALSHA` per value in a single round trip, so 60 values cost 0.7 to 1.0 ms against 2.1 to 2.4 ms with concurrent calls (`BenchmarkRedisBatch`).

Run it with `scripts/bench.sh`; `go run ./cmd/benchreport -subject pseudonymbench results/<run>/*.json` prints this table.
<!-- overhead:end -->

### What it catches

Per-entity scores for the regex tier, measured through the proxy (send text, read what the upstream received). Recall by span overlap; "bytes left visible" is the share of PII bytes that no mask covered.

**Public data: [`nvidia/Nemotron-PII`](https://huggingface.co/datasets/nvidia/Nemotron-PII) test split, first 3,000 rows (CC-BY-4.0, synthetic text)**

| kind | gold spans | recall | bytes left visible | precision | F2 |
|---|---:|---:|---:|---:|---:|
| EMAIL | 1,221 | 0.998 | 0.1% | 1.000 | 0.998 |
| PHONE | 638 | 0.997 | 0.3% | 0.985 | 0.994 |
| SSN | 249 | 1.000 | 0.0% | 1.000 | 1.000 |
| CARD | 380 | 0.113 | 88.8% | 1.000 | 0.138 |
| IBAN | 0 | n/a | n/a | n/a | n/a |

The card row is not a detector failure in the usual sense. Only 41 of the 380 card numbers in this dataset pass the Luhn check (10.8%); the text generator invented the rest, and a real card number always passes. The detector produced 41 card matches: exactly the valid ones. Dropping the check would raise recall on this data and mask many more order IDs on real data. Another 23,038 gold spans (names, addresses, dates, account numbers, ...) are outside what a regex tier targets and are not scored. The dataset has no IBAN labels.

**Synthetic set: 2,000 seeded samples with look-alike negatives** (values follow the same public rules the detectors check, so recall here is a self-check; the point is the precision column)

| kind | gold spans | recall | precision | F2 |
|---|---:|---:|---:|---:|
| EMAIL | 637 | 1.000 | 1.000 | 1.000 |
| PHONE | 551 | 1.000 | 0.866 | 0.970 |
| SSN | 568 | 1.000 | 1.000 | 1.000 |
| CARD | 614 | 1.000 | 0.978 | 0.995 |
| IBAN | 606 | 1.000 | 1.000 | 1.000 |

6.8% of PII-free samples had something masked. Almost all of that is 10-digit IDs that look like US phone numbers and 16-digit IDs that happen to pass Luhn: a checksum alone lets about one random digit string in ten through.

The same data through Presidio and through LiteLLM is in the next section.

### Against LiteLLM with its Presidio guardrail

The comparison people ask for: the same harness pointed at [LiteLLM](https://github.com/BerriAI/litellm) 1.106.0 with its Presidio guardrail (`mode: pre_call`, `output_parse_pii: true`, so masked values are restored in the reply) in front of the Presidio analyzer (spaCy `en_core_web_lg` 3.8.0) and anonymizer, all stock images, defaults left alone, digests in [`results/rivals-overhead/images.txt`](results/rivals-overhead/images.txt), configuration in [`bench/rivals/litellm/`](bench/rivals/litellm/). This proxy runs in a container with the in-memory vault. Everything on the proxy side sits behind Docker Desktop's network on the same Apple M4 laptop; the mock upstream and the load generator run natively, so both proxies pay the same extra hop. Reproduce with `scripts/rivals.sh`.

This is not a contest between equals. Presidio recognises names, addresses, dates and a long list of other entity types that this proxy does not look for, and LiteLLM is a general gateway doing far more than masking. What the tables show is what each costs and covers *for the five kinds this repository scores*.

**Streaming: time to first token** (2 req/s for 60 s after a 6 s warmup, 120 requests per cell, no errors, mock overflow 0; `results/rivals-lowrate/`). 2 req/s is slow enough that the analyzer is not saturated, so this isolates what LiteLLM does with a stream:

| payload | direct to the mock: p50 / p99 (ms) | pseudonymbench (container) | LiteLLM + Presidio |
|---|---:|---:|---:|
| pii (5 values) | 152.6 / 214.9 | 155.0 / 198.5 | **3318.1 / 3651.4** |
| pii-long (about 20 values) | 153.0 / 156.7 | 155.4 / 159.6 | **3359.8 / 3942.2** |

LiteLLM's first byte arrives at the same moment as its last: its time to first token equals the whole completion (about 3.3 s here, which is the mock's 150 ms prefill plus 240 chunks 12 ms apart). I checked that by hand too: one request, first byte after 3.6 s through LiteLLM against 0.16 s through this proxy and direct. I also checked where the hold comes from, three requests each: the same LiteLLM with the guardrail block removed streamed normally (first byte 0.17 s), and with `output_parse_pii: false` (placeholders stay in the reply, nothing is restored) the first byte still took 3.4 s. So in this version the guardrail holds the stream whether or not it restores values. This proxy adds about 2.5 ms at the median, in the same range as in the native runs plus a container hop. The direct run's p99 in the first row (214.9 ms) is a one-off spike on the baseline itself; read the p50 column.

**JSON responses and the load that saturates the stock analyzer** (10 req/s, 40 s, 400 requests per cell; `results/rivals-overhead/`):

| payload | mode | LiteLLM + Presidio: added p50 / p99 (ms) | pseudonymbench (container): added p50 / p99 (ms) |
|---|---|---:|---:|
| pii | JSON | +32.8 / +50.2 | +3.1 / +3.9 |
| pii-long | JSON | +69.3 / +110.9 | +3.2 / +5.5 |
| pii | stream, TTFT | +3242 / +4925 | +2.5 / +7.2 |
| pii-long | stream, TTFT | +6220 / +8562 | +2.8 / +7.0 |

The container proxy's +3 ms (against +0.5 ms native, above) is the Docker hop; the baseline goes straight from the host to the mock. The pii-long stream row is worse than the 2 req/s run because the stock analyzer runs one worker and queues at 10 req/s: that row measures a default deployment, not a tuned one. A tuned deployment would add workers; I did not tune it.

**Detection** on the first 3,000 Nemotron-PII test rows, recall by span overlap, from the outside (what the upstream received). "Presidio alone" calls the analyzer directly (`eval presidio`, `results/presidio/`); LiteLLM's numbers match it at score threshold 0.

| kind | gold spans | this proxy | Presidio analyzer alone | LiteLLM + Presidio |
|---|---:|---:|---:|---:|
| EMAIL | 1,221 | 0.998 | 0.998 | 0.998 |
| PHONE | 638 | 0.997 | 0.998 | 0.998 |
| SSN | 249 | 1.000 | 1.000 | 1.000 |
| CARD | 380 | 0.113 | 0.687 | 0.687 |
| share of replacements that overlap a labelled span | | 0.995 | 0.526 | 0.522 |

- **Cards:** Presidio masks 69% of the card numbers against 11% here, because 89% of this dataset's card numbers fail the Luhn check and this detector requires it. Presidio does not call them cards: of 380, LiteLLM's placeholders say card for 41, phone for 52, and something else for 168; 119 are left in the text.
- **Precision:** about half of what Presidio replaces overlaps no labelled span at all (and thousands more replacements fall on names and addresses, which the dataset labels but this benchmark does not score). On the synthetic set, whose PII-free samples are full of look-alike dates, version strings and tracking numbers, Presidio alone masked something in 88.8% of them against 6.8% for the regex tier, and its phone and SSN precision were 0.63 and 0.73 against 0.87 and 1.00. That is Presidio treating dates and IDs as PII, which is a different definition from this benchmark's; do not read it as an error rate. It does mean the model loses more context.
- **Whitespace:** LiteLLM drops the space that followed a placeholder, which wrecks exact-span scoring (0.05 exact-span recall on emails against 0.998 by overlap). Read the overlap column.

### Which request fields leak

A distinct canary email is planted in each of 21 request fields and the body the upstream received is checked. The control sends the same requests straight to the mock. The middle column is this proxy in a container; the right-hand pair are LiteLLM 1.106.0 with its Presidio guardrail as configured in [`bench/rivals/litellm/`](bench/rivals/litellm/config.yaml) and the control. Native and containerised runs of this proxy give the same column (`results/eval-synthetic/`, `results/rivals-nemotron/`).

| request field | pseudonymbench | LiteLLM + Presidio | no proxy (control) |
|---|---|---|---|
| `messages[].content` (string) | masked | masked | LEAKED |
| `messages[].content[].text` | masked | masked | LEAKED |
| system message | masked | masked | LEAKED |
| developer message | masked | masked | LEAKED |
| assistant message | masked | masked | LEAKED |
| assistant refusal | masked | LEAKED | LEAKED |
| `tool_calls[].function.arguments` (value) | masked | LEAKED | LEAKED |
| `tool_calls[].function.arguments` (key) | masked | LEAKED | LEAKED |
| tool message content | masked | masked | LEAKED |
| `tools[].function.description` | masked | LEAKED | LEAKED |
| `tools[].function.parameters` (schema text) | masked | LEAKED | LEAKED |
| `functions[].description` (legacy) | masked | LEAKED | LEAKED |
| `user` | masked | dropped | LEAKED |
| `metadata` (value) | masked | dropped | LEAKED |
| `metadata` (key) | masked | dropped | LEAKED |
| `stop` | masked | LEAKED | LEAKED |
| `prediction.content` | masked | LEAKED | LEAKED |
| `response_format.json_schema.description` | masked | LEAKED | LEAKED |
| `image_url.url` (http, query string) | masked | LEAKED | LEAKED |
| unknown top-level field | masked | LEAKED | LEAKED |
| unknown top-level field (name) | masked | LEAKED | LEAKED |

**12 of 21 fields reach the upstream through LiteLLM + Presidio unchanged; none do through this proxy.** Two caveats on reading the middle column. First, "dropped" means the field never arrived at the upstream (in this configuration LiteLLM did not forward `user` or `metadata`, checked by hand against the mock); nothing leaks, but nothing was masked either, and the request the model sees is not the one the client sent. Second, the guardrail's [documentation page](https://docs.litellm.ai/docs/proxy/guardrails/pii_masking_v2) does not say which request fields it scans, so the twelve "LEAKED" cells are a statement about this version as configured, not a claim that it breaks a documented promise. The canary is a plain `@example.com` address; a recognizer-based detector can only find what it recognizes, so a different kind of PII in those fields could behave differently.

The same command can fill in a column for any other proxy that can be pointed at the mock: `scripts/eval.sh` with `RIVALS=...`, or `scripts/rivals.sh` for the container stack.

## Quick start

```bash
# 1. a stand-in LLM API that echoes what it receives
go run ./cmd/mockupstream -listen :9000 -first-token 100ms -token-delay 20ms &

# 2. the proxy in front of it (in-memory vault)
go run ./cmd/pseudonymbench -listen :8080 -upstream http://localhost:9000 &

# 3. send a prompt
curl -s localhost:8080/v1/chat/completions -H 'Content-Type: application/json' \
  -d '{"model":"m","messages":[{"role":"user","content":"Email john@acme.com about card 4111 1111 1111 1111"}]}'
```

The reply contains the original values; the mock (and in real use, the provider) only ever saw `<EMAIL_...>` and `<CARD_...>`. Add `"stream": true` to see the streaming path. To use a real API, set `-upstream https://api.openai.com`; clients keep sending their own `Authorization` header.

With Docker: `export PSEUDONYMBENCH_KEY=$(openssl rand -hex 32) && docker compose up --build` starts the proxy with a Redis vault and the mock.

Useful flags: `-policy CARD=block,PHONE=off` (per-kind `mask`, `block` or `off`), `-vault redis -redis-url ...`, `-ttl 15m`, `-audit file|-|off`. Send an `X-Pseudonym-Session` header to keep the same value on the same token across a conversation. `pseudonymbench -h` lists everything.

## How it works

```mermaid
sequenceDiagram
    participant App
    participant PB as pseudonymbench
    participant V as Vault
    participant LLM as Upstream LLM API
    App->>PB: prompt with john@acme.com
    PB->>PB: walk every string in the JSON body, detect PII
    PB->>V: token for (session, EMAIL, value)
    PB->>LLM: same request, <EMAIL_7f3a9c21> instead
    LLM-->>PB: reply (JSON or SSE) mentioning the token
    PB->>V: restore (request-local cache first)
    PB-->>App: reply with john@acme.com
```

- **Detection** is regex plus validation, no ML: Luhn and an issuer table for cards, mod-97 and per-country length for IBANs, SSA structure rules for SSNs, NANP and E.164 shapes for phones. Details and the overlap rule: [docs/design.md](docs/design.md).
- **Masking is default-scan.** Every string in the request is scanned except a short list of fields the API validates (`model`, `role`, function names, base64 data). Unknown fields are scanned, not skipped. Tool-call `arguments` (JSON inside a string) are parsed and scanned.
- **The stream rewriter** holds back only a trailing piece of text that could still grow into a token (at most 15 bytes), per choice and per field, and flushes it on the finish chunk, `[DONE]`, an error frame or end of input. Ordinary prose passes through in the event it arrived in. It is tested at every split position, with random chunking, with `n>1`, with parallel tool calls, and by a fuzz target that compares chunked output to whole-text restoration.
- **Fails closed.** Invalid JSON, a compressed body, an oversize body, an unknown path, a blocked kind or a vault error returns an error; nothing is forwarded.
- **Vault.** In memory or Redis. Redis holds AES-256-GCM ciphertext (bound to session and token) and HMAC-keyed value hashes, never plaintext, and allocates tokens with a single Lua script (or an `HSETNX` sequence with `-redis-lua=false`; measured 2.6x slower per new value). Against Redis, all of a request's tokens are allocated in one pipelined round trip (`-redis-batch=false` for concurrent single calls).
- **Audit log.** One JSON line per request: kinds, counts, timings, outcome. Never values, prompts or headers (a test greps for them).

## What it misses

The full table with evidence is in [docs/threat-model.md](docs/threat-model.md). The ones that matter most:

| Miss | Notes |
|---|---|
| Names, addresses, dates of birth, free-text identifiers | No NER. 23,038 of the roughly 25,500 labelled spans in the Nemotron sample are of this kind. |
| Card numbers that fail Luhn | By design; see the card row above. |
| Look-alikes | 10-digit IDs starting 2-9 and Luhn-passing 16-digit IDs get masked. Reversible, but they cost the model context. |
| Tokens the model changes | Restoration is exact-match. A lowercased, spaced, truncated or paraphrased token stays as the model wrote it; it is never restored to a wrong value (tested). |
| Tokens in links | Tokens inside a URL are deliberately left unrestored so a prompt-injected model cannot smuggle a value into a link. Best effort: links without a scheme and non-URL channels are not covered. |
| The client's query string | Not forwarded (the upstream URL's own query string is). It is a request field the masker never sees. |
| Images, audio, files; other APIs | Only text in chat-completions. Anthropic Messages, the Responses API and other dialects return 403. |
| Quasi-identifiers | ZIP + gender + birth date identify most Americans without any masked field. Out of scope. |
| Size and timing side channels | Not padded. |

## Reproduce

```bash
go test -race ./...                 # unit, integration and property tests
make fuzz                           # 10 s on each fuzz target (FUZZTIME=2m for longer)

scripts/bench.sh                    # overhead matrix -> results/<run>/REPORT.md
scripts/eval.sh                     # detection quality + field matrix on synthetic data
go run ./cmd/dataprep nemotron -n 3000 && DATA=data/nemotron-pii.jsonl scripts/eval.sh
```

Requirements: Go 1.26, `curl`, `openssl`. Redis rows and rivals need Docker; see [docs/benchmarking.md](docs/benchmarking.md) and `bench/rivals/litellm/`.

```bash
REDIS_URL=redis://localhost:6379/0 scripts/bench.sh   # Redis rows: HSETNX, Lua with concurrent calls, Lua pipelined
export PSEUDONYMBENCH_KEY=$(openssl rand -hex 32)
docker compose -f bench/rivals/litellm/docker-compose.yml up -d --build
scripts/rivals.sh                                     # LiteLLM + Presidio vs this proxy: detection, fields, overhead
```

## Repository layout

```
cmd/pseudonymbench   the proxy
cmd/mockupstream     fake LLM API with fixed timing; records what it receives
cmd/loadgen          open-loop load generator (latency, time to first token)
cmd/benchreport      results -> Markdown table of deltas against a baseline
cmd/eval, dataprep   detection scoring, field matrix, public dataset converter
internal/detect      PII detectors        internal/token   token format, stream hold-back rule
internal/mask        request masking, response restoration, URL guard
internal/stream      SSE rewriter         internal/vault   memory + Redis vaults, AES-GCM
internal/proxy       HTTP handler, audit  internal/eval    scoring, field matrix, diffing
docs/                design, threat model, benchmarking method, spec, research notes
```

## Related work

Several open-source proxies already mask and restore PII for OpenAI-style APIs. As of 2026-10-19, from reading their READMEs and source (see [docs/research](docs/research/)), none of those I read published all of: open-loop overhead against a direct baseline, a time-to-first-token delta, per-entity precision and recall on a redistributable dataset, and a field-coverage matrix. That is a statement about what I found, not a proof.

| Project | What I found |
|---|---|
| [PrivAiTe](https://github.com/crp4222/PrivAiTe) | Streaming restore, a published scanned/not-scanned field list for two APIs, an agent-workflow latency benchmark; no TTFT; its main accuracy corpus is not redistributable |
| [LLM-Shield-Proxy](https://github.com/ninadphalak/LLM-Shield-Proxy) | Sliding-window SSE restore; timings are in-process microseconds and say they are not end-to-end |
| [Tamga](https://github.com/yatuk/tamga) | Go gateway; k6 open-loop load results, but with a no-PII payload and no direct baseline |
| [PasteGuard](https://github.com/sgasser/pasteguard) | Mask and restore with streaming; ships an accuracy harness, no committed results |
| [Anwesha33/pii-shield](https://github.com/Anwesha33/pii-shield) | A Go proxy with a plan close to this one's. At the commit I read it held back text for `content` only, shared one buffer across choices, and published detector latency, not proxy overhead |
| [LiteLLM Presidio guardrail](https://docs.litellm.ai/docs/proxy/guardrails/pii_masking_v2) | Masks and restores. Measured here on 1.106.0 (above): the stream is held until the reply is complete, and 12 of 21 request fields were not masked |
| [LLM Guard](https://github.com/protectai/llm-guard) | Anonymize/deanonymize scanners as a library; archived 2026-07 |

A different project named `maskgate` (`tsymbaluyk/maskgate`) exists; this repository is unrelated to it, which is why it is called pseudonymbench.

## Limits and wording

- It is a benchmarked research tool, not production-grade software, and nothing here is a statement about legal compliance.
- The proxy is a fully trusted component: it sees every prompt in plaintext and the `Authorization` header. Run it inside your trust boundary behind TLS.
- Overhead numbers are from one laptop. The Redis and the LiteLLM/Presidio runs used Docker Desktop's VM on it, which adds a hop and some noise; a quiet Linux host with pinned CPUs is still to do. The LiteLLM stack was run as shipped and untuned, on one version, and its streaming and field behaviour is what that version did on 2026-10-20, not a statement about other versions.
- Dataset: Nemotron-PII is by NVIDIA, licensed CC-BY-4.0; it is fetched on demand and not stored in this repository.
- Licensed under the Apache License 2.0; see [LICENSE](LICENSE).
