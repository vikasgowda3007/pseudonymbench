# pseudonymbench: spec v0

Status: draft, 2026-10-19. Supersedes the original "maskgate" plan. Evidence is in `docs/research/maskgate-brainstorm.md` and the gap check at `career-ops/output/research-maskgate-gap-check-2026-10-19.md`.

## Name

`pseudonymbench`. Checked on GitHub on 2026-10-19: no repo with that exact name. "maskgate" is taken (`tsymbaluyk/maskgate`, 247 stars, a proprietary PII masker). "maskbench", "redactbench" and "scrubline" are used by unrelated repos. Fallbacks that were also free: `roundtripbench`, `scrubbench`, `redactrace`. Not checked: domains, trademarks.

The working directory is still `maskgate/`. The repo has no commits and no remote, so rename the folder before the first push.

## Pitch

A reproducible benchmark, plus a small Go reference proxy, for reversible PII masking in front of OpenAI-style chat-completions APIs.

It measures four things that, in the repos inspected on 2026-10-19, nobody publishes together:

| # | Measurement | Closest existing work |
|---|---|---|
| a | Proxy overhead p50/p99 at a fixed open-loop arrival rate, against a direct-to-upstream baseline, on PII-bearing payloads | Tamga: open-loop, but a no-PII payload, no direct baseline, no streaming |
| b | Streaming time-to-first-token (TTFT) delta against that baseline | Nobody found |
| c | Per-entity precision/recall measured through the proxy path, on a redistributable dataset | veil: self-generated, saturated at 1.000. PrivAiTe: AI4Privacy rows, not redistributable |
| d | Field-coverage matrix: which request fields get masked, verified by planted canaries, across several proxies | PrivAiTe and LLM-Shield-Proxy publish lists for themselves only |

Claim wording: "In the repos I inspected on 2026-10-19, none published this combination." Never "first", "production-grade" or "compliant". Use "pseudonymization, not anonymization" and "benchmarked on a synthetic/public set".

The proxy is the test subject. The README leads with the results table, not the proxy.

## Scope

In:
- OpenAI chat-completions only, stream and non-stream.
- Regex tier: email, phone, SSN-shaped, card (Luhn plus issuer prefix and length check), IBAN (mod-97).
- JSON-aware masking of `messages[].content` (string and parts array), `tool_calls[].function.arguments`, tool messages, and tool schema descriptions.
- Per-session vault behind an interface: in-memory first, Redis second. Random tokens like `<EMAIL_7f3a>`, per-session namespace, TTL, AES-GCM on stored values.
- Fail closed: a body that can't be parsed is rejected, not forwarded.
- Streaming rewriter with fuzz tests.
- Benchmark harness, miss table, threat model.

Out (cut unless time is left): Presidio sidecar (at most one optional benchmark row), format-preserving tokens, hash policy, Anthropic and Responses-API adapters, GIF, dashboards.

Language: Go. Keep detectors and the stream rewriter free of I/O so they can be tested and fuzzed in isolation.

## Harness design

- **Mock upstream:** emits SSE chunks with a configurable inter-token delay (start with 20–50 ms) and configurable response length, including responses that echo tokens. Without realistic delay, TTFT numbers mean nothing.
- **Load client:** a small Go program that dispatches on a fixed schedule (open-loop) and measures latency from the intended send time, to avoid coordinated omission (wrk2 README). Written ourselves because native SSE and TTFT support in k6, vegeta and similar tools is unverified.
- **Overhead definition:** proxy end-to-end minus direct-to-same-mock at the same arrival rate. Report p50, p99, p99.9 at several rates.
- **Payload sets:** no PII; PII-bearing; tool-call arguments; long stream; split-token stress.
- **Rows:**
  1. Direct baseline
  2. This proxy, regex tier, in-memory vault
  3. This proxy, Redis vault
  4. LiteLLM + Presidio guardrail
  5. Stretch: one more rival (PasteGuard or PrivAiTe)
- **Rival rule:** rivals ship with different detectors, so report each "as shipped" and publish its config next to its numbers. This is not a detection head-to-head.
- **Environment:** Linux, not Docker on macOS. Record CPU, kernel and versions.

## Per-entity precision/recall

- Entities: EMAIL, PHONE, SSN, CARD, IBAN only. The regex tier will have near-zero recall on names and addresses, and the README says so.
- Data: Nemotron-PII (CC-BY-4.0) and/or gretel-pii-masking-en-v1 (Apache-2.0), plus generated Luhn-valid cards and mod-97 IBANs, because the public label sets have no IBAN. **Check both dataset cards before relying on them**: the gap check could not confirm the Nemotron licence.
- Measured through the proxy path: send the text, capture what the mock upstream received, compare to labelled spans. Report exact-span and overlap matching separately.
- Do not use ai4privacy 200k/300k/400k: not redistributable.

## Field-coverage matrix

Plant a unique canary in each field, send it through each proxy, and record what the mock upstream received: masked, leaked or skipped.

Request fields: `messages[].content` (string and parts), `messages[].name`, system message, assistant `tool_calls[].function.arguments`, tool message content, `tools[].function.description` and parameter schemas, `user`, `metadata`, `logit_bias` keys. Response side: `delta.content`, tool-call argument deltas, `n>1` choices.

## Streaming rewriter

- Hold-back buffer keyed on a possible token prefix, bounded by the maximum token length.
- Separate channels per choice and per tool call. Flush on `finish_reason` and on `[DONE]`.
- Tool-call arguments are JSON inside JSON: handle escaping.
- Unknown token (model invented it): leave visible and count it.
- Usage chunk and mid-stream error frames pass through correctly.
- Tests: every split position of a token, random chunking, near-miss prefixes, `n>1` interleaving. Publish as a conformance suite other proxies can run.
- Exfiltration: under prompt injection the model can be told to put a token in a URL, and rehydration then restores the real value into it. Mitigation (partial): do not rehydrate inside URLs or markdown link targets, or rehydrate only for allowlisted hosts. The streaming rewriter must decide before it has seen the whole URL, so state in the threat model that this is best-effort.

## Plan

Week 1: proxy, regex rules with Luhn/SSN/IBAN checks, JSON-aware masking, streaming rewriter, fuzz tests, in-memory vault. Fail-closed handling.

Week 2: mock upstream, load client, Redis vault, baseline and proxy rows, LiteLLM row, per-entity P/R, field matrix, miss table, threat model, README with the table above the fold.

Stretch: a second rival row.

**Kill line:** end of week 1, if fuzz tests do not pass for `delta.content` and tool-call arguments with `n=1` and tokens split across chunks, stop and move to smsroute-lite.

## README outline

1. One-line value statement and the results table (a–d).
2. How to reproduce (one command per table).
3. Architecture diagram.
4. What it misses (measured where possible).
5. Threat model: echoed or transformed tokens, quasi-identifier re-identification, PII in unscanned fields, memorized or hallucinated PII in output, token-guessing across sessions, URL exfiltration (partial mitigation), vault compromise, log leakage, the proxy as a trusted party.
6. Method and caveats, versions, dates.

## Open items to verify

- Dataset licences (Nemotron-PII, gretel, openpii) on their cards.
- Whether k6 or another tool can measure SSE TTFT natively. If so, drop the custom client.
- LiteLLM version and Presidio config to benchmark, and whether the whole-stream buffering still occurs on that version.
- Gap claims are from default-branch HEAD of shallow clones: no releases, wikis or history were read.
- ISO 7812-1 (card), ISO 13616 (IBAN) and SSA pages returned 403 during research. Checksum rules rest on Presidio's implementation and POMS RM 10201.035.
