# pseudonymbench: spec v1

Status: v1, 2026-10-19. Supersedes the first draft and the original "maskgate" plan.
Evidence: [`research/maskgate-brainstorm.md`](research/maskgate-brainstorm.md) (prior art, streaming, detection, Presidio, datasets, threat model, language) and [`research/gap-check-2026-10-19.md`](research/gap-check-2026-10-19.md) (do existing proxies publish rigorous evaluation?).
How it is built: [`design.md`](design.md). How it is measured: [`benchmarking.md`](benchmarking.md). What it does not protect: [`threat-model.md`](threat-model.md).

## Name

`pseudonymbench`. The first name, "maskgate", is taken: `tsymbaluyk/maskgate` (247 stars, a proprietary Russian-market PII masker, hosted at maskgate.ru). On 2026-10-19 no GitHub repo had the name `pseudonymbench`. `maskbench`, `redactbench` and `scrubline` are used by unrelated repos; `roundtripbench`, `scrubbench` and `redactrace` were also free. Domains and trademarks were not checked. The Go module is `github.com/vikasgowda3007/pseudonymbench`; change the owner segment if the repo moves.

## Pitch

A reproducible benchmark, plus a small Go reference proxy, for reversible PII masking in front of OpenAI-style chat-completions APIs.

It measures four things that, in the repos inspected on 2026-10-19, nobody publishes together:

| # | Measurement | Closest existing work |
|---|---|---|
| a | Proxy overhead p50/p99 at a fixed open-loop arrival rate, against a direct-to-upstream baseline, on PII-bearing payloads | Tamga: open-loop, but a no-PII payload, no direct baseline, no streaming |
| b | Streaming time-to-first-token (TTFT) delta against that baseline | Nobody found |
| c | Per-entity precision/recall measured through the proxy path, on a redistributable dataset | veil: self-generated, saturated at 1.000. PrivAiTe: AI4Privacy rows, not redistributable |
| d | Field-coverage matrix: which request fields get masked, verified by planted canaries, across proxies | PrivAiTe and LLM-Shield-Proxy publish lists for themselves only |

The gap check is partial evidence: a shallow read of the default branch of about twenty repos, no releases, wikis or history. Its own table missed `Anwesha33/pii-shield` (a Go LLM proxy), which the first research found; the gap check's "correction" was about a different repo with the same name (`pii-shield/pii-shield`, a Kubernetes log sidecar). Treat "nobody publishes this" as "I did not find it".

Claim wording: "In the repos I inspected on 2026-10-19, none published this combination." Never "first", "production-grade" or "compliant". Use "pseudonymization, not anonymization" and "benchmarked on a synthetic/public set". The README leads with results, not features; the proxy is the test subject.

## Decisions

| Decision | Choice | Reason |
|---|---|---|
| Language | Go | Small, I/O-bound, concurrent; `net/http` streaming, RE2 (no ReDoS), native fuzzing. Python's main edge was in-process Presidio, which is out of the MVP. |
| API surface | OpenAI chat-completions only, stream and non-stream | Each other SSE dialect (Anthropic, Responses API) is a separate bug class. Other paths return 403 rather than being forwarded unscanned. |
| Detection | Regex tier: email, phone, SSN/ITIN-shaped, card (Luhn + issuer table), IBAN (mod-97) | A measured baseline first. Names and addresses need NER. |
| Token | `<KIND_xxxxxxxx>`, 32 random bits | Random (reveals nothing), typed, bounded length. The first draft said 4 hex digits; the research shows 16 bits collides at ~100 values per session. |
| Vault | Interface; memory and Redis; AES-256-GCM with session+token as AAD; HMAC value index; no Lua | `HSETNX` is enough for random tokens. |
| Failure mode | Fail closed | An unscannable body is rejected, never forwarded. |
| Exfiltration | Tokens inside URLs are not restored (best effort) | OWASP LLM01 image-link scenario. |
| Metrics | recall (overlap, exact), bytes left visible, typed precision, F2, clean-text false-positive rate | Per research section 7.2. |
| Datasets | Synthetic (seeded, in-repo) and `nvidia/Nemotron-PII` (CC-BY-4.0, fetched on demand) | Licences confirmed from the dataset cards. Gretel (Apache-2.0) has no span offsets; ai4privacy 200k/300k/400k cannot be redistributed. |

Cut from the original plan: Presidio sidecar in the proxy, format-preserving tokens, `hash` policy action, Anthropic/Responses adapters, GIF, dashboards. Presidio is available as an **evaluation** row (`eval presidio`) so "regex tier vs regex + Presidio" can still be compared on the same data without putting it in the request path.

## Harness design

- **Mock upstream** (`cmd/mockupstream`): fixed prefill delay, fixed per-chunk delay, reply padded to a fixed length so timing never depends on the request. It echoes the PII values or tokens it finds, so the proxy has tokens to restore, and it counts replies whose echo overflowed the fixed length (an overflow biases the comparison; the first full run was wrong for exactly this reason).
- **Load client** (`cmd/loadgen`): open-loop, latency measured from the intended send time, TTFT measured at the first content delta, marked invalid when it falls behind its own schedule. Written because the SSE timing support of k6, vegeta, wrk2 and oha is not documented.
- **Overhead** = proxy minus direct-to-same-mock at the same payload, mode and rate, per percentile. A second baseline run (`direct-repeat`) gives the noise floor.
- **Payload sets**: plain, pii, pii-long, tools. Values regenerated per request from a seeded generator.
- **Rows**: direct baseline; this proxy with the in-memory vault; this proxy with the Redis vault (needs Redis); LiteLLM + Presidio guardrail (template in `bench/rivals/litellm/`, untested); optionally one more rival.
- **Rival rule**: rivals ship different detectors, so report each "as shipped" with its configuration. Overhead is comparable; detection is not a like-for-like contest.
- **Environment**: record CPU, OS and versions with every result. Publishable numbers need a quiet Linux host; laptop runs are labelled as such.

## Evaluation design

- Entities: EMAIL, PHONE, SSN, CARD, IBAN. Other PII in a dataset is tagged `OTHER`: neither a hit nor a miss, and masking over it is not a false positive.
- Black-box: send text through the proxy, read what the mock received, align with the original (word-level LCS), score gold spans against changed regions. A test checks this equals running the detectors directly.
- Field matrix: a distinct canary email in each of 20 request fields; `masked`, `LEAKED` or `rejected`; a no-proxy control must leak all 20. `messages[].name` and `logit_bias` keys are not probed (API character restrictions); response-side restoration is covered by the stream and proxy tests.

## Status against the plan

| Item | State |
|---|---|
| Detectors, token, vault (memory, Redis via miniredis), masker, stream rewriter, proxy | Done, tested with the race detector; fuzz targets for detect, token, mask, stream |
| Kill line (fuzz-tested rewriter for `delta.content` and tool arguments, `n=1`, split tokens) | Passed |
| Harness: mock, open-loop load generator, report, scripts | Done |
| Eval: scoring, field matrix, Nemotron converter, Presidio client | Done |
| Laptop benchmark with the fixed mock | See `results/` and the README |
| Redis row, LiteLLM row, Presidio row | **Not run**: no Docker daemon was available. Templates and code exist; numbers do not |
| Quiet-host Linux numbers | Not done |
| Licence file | Not chosen: the repo owner decides |

## Open items

- Run the Redis, LiteLLM and Presidio rows on a host with Docker; pin image digests and publish configs next to the numbers.
- Re-run overhead on a quiet Linux host with pinned CPUs and at more than one rate; collect enough samples for p99.9.
- Whether k6 (or another tool) can measure SSE TTFT natively is still unverified. If so, the custom load client could be dropped.
- Whether LiteLLM still buffers the whole stream on the version benchmarked.
- ISO/IEC 7812-1 (card), ISO 13616 (IBAN) and the SSA randomization pages returned 403 during research. Checksum rules rest on Presidio's implementation, POMS RM 10201.035, and public issuer documentation.
- Gap-check claims come from the default branch of shallow clones; re-run before publishing a "nobody does this" sentence.
- Pick a licence.
