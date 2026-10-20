# maskgate: research brainstorm

Date checked: 2026-10-19 (versions, star counts, repo states and docs all reflect this date).

Scope: a reverse proxy in front of OpenAI-style chat-completions APIs that swaps PII for tokens before the request leaves, stores the mapping in Redis, and swaps real values back into the response (including SSE streams).

How to read the tags in this file:

- **[src@sha]** = I read the code in a shallow clone at that commit (link points at the same commit). I read it; I did not run it unless a **[my check]** tag says so.
- **[my check]** = I ran a small calculation or regex test locally (scratch directory, nothing added to this repo).
- **[vendor-claim]** = a project's own README or doc statement that I did not reproduce.
- **[analysis]** = my reasoning built on the cited facts; the reasoning itself is not sourced.
- **UNVERIFIED** = see the last section.
- Pages fetched with WebFetch were summarised by a small model. I only quote short fragments; re-open the URL before quoting it in the README.

---

## Bottom line

1. **Do not pitch maskgate as a new idea; the niche is occupied.** At least ten open-source reversible-masking proxies for OpenAI-style APIs exist on GitHub today, several in Go, one with a Redis session vault, one with a plan nearly identical to this one (details in section 1). Boundary-safe SSE re-hydration with exhaustive split tests is already shipped by at least two of them. Build only if you reframe it as a **measured engineering artifact**.
2. **The gap I could find (absence of evidence, not proof):** none of the 12 repos and docs I inspected publishes proxy-level overhead (open-loop p50/p99 added latency and time-to-first-token delta under load against a mock upstream), and none publishes per-entity precision/recall for a regex tier on a redistributable CC-BY corpus together with a field-coverage matrix (which request fields leak). That is the repo to build: proxy + reproducible harness + honest miss table.
3. **LiteLLM + Presidio overlaps only partly.** For OpenAI-format streams its guardrail buffers the whole response, unmasks it, and re-emits one synthetic chunk (so time-to-first-token equals full generation time); its token map is a per-request in-memory dict; its Anthropic SSE path replaces per delta with no cross-delta buffer; and there are open user-reported bugs on all of this. LLM Guard (archived 2026-07-09) has an in-memory vault and a whole-string deanonymizer with no streaming support.
4. **Language: Java 21 if the goal is a credible infra repo with p99 numbers in 2-3 weeks and Go ramp-up is uncertain; Go if the goal is a Go-shop signal.** The facts are close: Go's standard library does the SSE-proxy plumbing and RE2 cannot ReDoS; Java gives you lookaround and your own fluency, but you write more proxy plumbing. Keep the streaming rewriter and detectors as I/O-free modules so the language choice touches maybe a fifth of the work (section 9).
5. **MVP cut line:** OpenAI chat-completions only (stream and non-stream); regex tier (email, phone, SSN-shaped, Luhn-checked card with issuer/length check, IBAN mod-97); JSON-aware walk over messages, tool-call arguments and (by policy) tool schemas; per-session Redis vault with random tokens, TTL and AES-GCM values; fail-closed; boundary-safe streaming rewriter with per-choice and per-tool-call channels and property/fuzz tests; mock-upstream load harness; README with miss table. **Cut or defer:** Presidio sidecar (keep as one optional benchmark row), format-preserving tokens, hash policy, Anthropic/Responses adapters, GIF, dashboards.
6. **Biggest risks:** (a) differentiation/perception (section 10); (b) the streaming long tail is protocol breadth (tool-call arguments are JSON inside JSON, `n>1`, finish-reason ordering, mid-stream error frames), not the single split-token case; (c) rehydration creates an exfiltration channel under prompt injection (the model can be told to put tokens in a URL; the proxy then writes the real value into that URL); (d) benchmark credibility (dataset licences, synthetic labels, macOS Docker numbers); (e) scope creep from Presidio ops and FPE.
7. **Claims in the plan that look wrong or overstated:** Lua is not strictly required for atomic allocation (section 6); "official Presidio Docker images" moved from MCR to ghcr.io and the MCR images are no longer updated (section 4); LLM Guard is archived; FF3-1 is not a safe choice (NIST's 2025 draft drops FF3 and says the tweak-schedule weakness affected FF3 and FF3-1, not FF1); ai4privacy pii-masking-200k/300k/400k cannot simply be redistributed in a repo (section 7); "any LLM API" is overstated (OpenAI chat-completions is one of at least three SSE dialects); a hash policy for SSNs is reversible by enumeration unless keyed (section 5); "deterministic per-session tokens" and "tested streaming state machine" are not unique (section 10).
8. **Wording to keep:** "pseudonymization, not anonymization", "benchmarked on a synthetic set", never "production-grade" or "compliant".

---

## 1. Prior art / overlap

### 1.1 Hosted and library tools

Every cell links to the source I read. "Not stated" means the page I read does not say; it is not a claim that the feature is absent.

| Tool | (a) masks requests | (b) re-hydrates responses | (c) SSE streaming re-hydration | (d) persistent vault | (e) published benchmark numbers |
|---|---|---|---|---|---|
| **Presidio** (analyzer + anonymizer; now community-owned at data-privacy-stack, MIT) | Yes: operators replace, redact, hash, mask, encrypt, custom, keep ([anonymizer docs](https://presidio.dataprivacystack.org/anonymizer/)) | Only `decrypt` reverses, with a shared AES key; REST `/deanonymize` exists ([anonymizer docs](https://presidio.dataprivacystack.org/anonymizer/)); `encrypt` is AES-CBC, random IV, no integrity tag ([aes_cipher.py@4f0e236](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-anonymizer/presidio_anonymizer/operators/aes_cipher.py)) | No; first-party sample "Invisio" does session-based anonymize/de-anonymize but has no streaming handling ([sample@4f0e236](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/samples/deployments/openai-anonymaztion-and-deanonymaztion-best-practices/index.md)) | Library is stateless; the Invisio sample lists Redis in its architecture ([same sample](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/samples/deployments/openai-anonymaztion-and-deanonymaztion-best-practices/index.md)) | No latency or accuracy tables in docs; recipe template leaves "Precision: TBD" ([recipe](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/recipes/german-language-support/README.md)); notebook outputs in section 4 |
| **LiteLLM Presidio guardrail** | Yes; tokens `<ENTITY_N>`; mask or block per entity ([docs](https://docs.litellm.ai/docs/proxy/guardrails/pii_masking_v2), [source@2d181e6 L856-L898](https://github.com/BerriAI/litellm/blob/2d181e663373/litellm/proxy/guardrails/guardrail_hooks/presidio.py#L856-L898)) | Yes with `output_parse_pii`, only for Presidio `replace`; the flag "does not run Presidio detection on outputs" ([docs](https://docs.litellm.ai/docs/proxy/guardrails/pii_masking_v2)) | Partial. OpenAI-format streams are fully buffered, assembled, unmasked and re-emitted as one synthetic chunk ([`_stream_pii_unmasking` L1639-L1712](https://github.com/BerriAI/litellm/blob/2d181e663373/litellm/proxy/guardrails/guardrail_hooks/presidio.py#L1639-L1712)); Anthropic raw SSE is replaced per `text_delta` with no cross-delta buffer ([L1586-L1613](https://github.com/BerriAI/litellm/blob/2d181e663373/litellm/proxy/guardrails/guardrail_hooks/presidio.py#L1586-L1613)); open user-reported bugs: [#46087](https://github.com/BerriAI/litellm/issues/46087), [#42476](https://github.com/BerriAI/litellm/issues/42476), [#41611](https://github.com/BerriAI/litellm/issues/41611) | No: `pii_tokens` lives in `request_data["metadata"]`, per request ([L856-L898](https://github.com/BerriAI/litellm/blob/2d181e663373/litellm/proxy/guardrails/guardrail_hooks/presidio.py#L856-L898)) | None found on the docs page ([docs](https://docs.litellm.ai/docs/proxy/guardrails/pii_masking_v2)) |
| **Portkey** PII redaction guardrail | Yes, `{{EMAIL_ADDRESS_1}}` style ([docs](https://portkey.ai/docs/product/guardrails/pii-redaction)) | No: "Redaction is irreversible by design" ([docs](https://portkey.ai/docs/product/guardrails/pii-redaction)) | Not stated ([docs](https://portkey.ai/docs/product/guardrails/pii-redaction)) | No mapping described ([docs](https://portkey.ai/docs/product/guardrails/pii-redaction)) | None on page |
| **Lakera Guard** | No: screening endpoint; `payload: true` returns PII spans, the app masks ([docs](https://docs.lakera.ai/docs/api/guard)) | No ([docs](https://docs.lakera.ai/docs/api/guard)) | n/a (API call, not a proxy) | n/a | None on page |
| **Protecto** | Yes: tokenize/mask API ([docs](https://docs.protecto.ai/)) | An "Unmask with Token API" exists; LLM-response rehydration flow not described ([docs](https://docs.protecto.ai/)) | Not stated | Not described | None |
| **Cloudflare AI Gateway** (DLP, Guardrails) | No masking: DLP detects, flags or blocks ([DLP](https://developers.cloudflare.com/ai-gateway/features/dlp/)); Guardrails evaluate harmful-content categories ([Guardrails](https://developers.cloudflare.com/ai-gateway/features/guardrails/)) | No | Streaming responses are buffered in full before DLP scanning, so time-to-first-token rises ([DLP](https://developers.cloudflare.com/ai-gateway/features/dlp/)) | n/a | None |
| **Kong AI Sanitizer** plugin | Yes: placeholder (`PLACEHOLDER{i}`) or synthetic modes via a separate AI PII Anonymizer Service ([docs](https://developer.konghq.com/plugins/ai-sanitizer/)) | Optional restoration, input sanitization only ([docs](https://developer.konghq.com/plugins/ai-sanitizer/)) | Not stated | Not stated | None |
| **LLM Guard** (Protect AI; **archived** 2026-07-09) | Yes: `[REDACTED_PERSON_1]` or Faker values ([anonymize.py@168c103](https://github.com/protectai/llm-guard/blob/168c1034ffdb/llm_guard/input_scanners/anonymize.py)) | Yes: Deanonymize with exact, case-insensitive or fuzzy matching over the whole string ([deanonymize.py](https://github.com/protectai/llm-guard/blob/168c1034ffdb/llm_guard/output_scanners/deanonymize.py)) | No streaming-aware deanonymizer; the streaming example accumulates output and re-scans it ([openai_streaming.py](https://github.com/protectai/llm-guard/blob/168c1034ffdb/examples/openai_streaming.py)) | `Vault` is an in-memory Python list ([vault.py](https://github.com/protectai/llm-guard/blob/168c1034ffdb/llm_guard/vault.py)); archived notice in [README](https://github.com/protectai/llm-guard/blob/168c1034ffdb/README.md) | Latency table for Anonymize on AWS instances (for example 177 ms average on m5.xlarge for a 317-char input), no precision/recall ([docs](https://github.com/protectai/llm-guard/blob/168c1034ffdb/docs/input_scanners/anonymize.md)) |
| **Guardrails AI** DetectPII | Yes (anonymizing programmatic fix, built on Presidio) ([hub](https://guardrailsai.com/hub/validator/guardrails/detect_pii)) | No unmask feature mentioned | Not stated | No | None on page |
| **NeMo Guardrails** | Yes: label placeholders such as `[FIRST_NAME]` via GLiNER, Presidio or Private AI flows ([PII docs@3a6e84f](https://github.com/NVIDIA-NeMo/Guardrails/blob/3a6e84f8e35a/docs/configure-rails/guardrail-catalog/pii-detection.mdx)) | No reversal documented (Presidio `replace` operators in [actions.py](https://github.com/NVIDIA-NeMo/Guardrails/blob/3a6e84f8e35a/nemoguardrails/library/sensitive_data_detection/actions.py)) | n/a | No | None in the PII page |
| **Helicone** | No PII masking feature found in the docs index or in a grep of the docs tree ([docs index](https://docs.helicone.ai/); I also grepped the `docs/` tree of [the repo](https://github.com/Helicone/helicone) and found only log-omission features, not masking) | n/a | n/a | n/a | n/a |
| *Extra:* **Amazon Bedrock Guardrails** | Mask to `{NAME}`/`{EMAIL}` or block; does not evaluate tool-call arguments, tool results or tool definitions; invocation logs keep originals ([docs](https://docs.aws.amazon.com/bedrock/latest/userguide/guardrails-sensitive-filters.html)) | No | n/a | n/a | None |
| *Extra:* **LangSmith LLM Gateway** | Yes; skips system and developer prompts, and tool-call arguments delivered as one JSON string ([docs](https://docs.langchain.com/langsmith/llm-gateway-redaction)) | Yes, successful responses only | Yes: "rewritten event by event" (mechanism not documented) ([docs](https://docs.langchain.com/langsmith/llm-gateway-redaction)) | Not stated | None |

### 1.2 Open-source reversible-masking proxies on GitHub (found by searching GitHub on 2026-10-19)

Search terms used: "pii proxy openai", "pii masking proxy llm", "pseudonymize llm proxy", "reversible pii llm", "llm privacy gateway redact restore", "pii gateway golang llm", "tokenization pii llm proxy", "llm pii sse streaming restore" (via `gh search repos`). Star counts and licences from the GitHub API the same day.

| Project (stars, licence, lang) | (a)/(b) mask + restore | (c) SSE re-hydration | (d) vault | (e) benchmark |
|---|---|---|---|---|
| [sgasser/pasteguard](https://github.com/sgasser/pasteguard) (767, Apache-2.0, TypeScript) | Yes ([README](https://github.com/sgasser/pasteguard/blob/95df2a7a2732/README.md)) | Yes: `StreamRestorer` per (choice, tool-call) channel, JSON-escapes restored values for tool arguments ([stream-transformer.ts](https://github.com/sgasser/pasteguard/blob/95df2a7a2732/src/providers/openai/stream-transformer.ts)) | Per-request in-memory `PlaceholderContext` ([context.ts](https://github.com/sgasser/pasteguard/blob/95df2a7a2732/src/masking/context.ts)); SQLite is for request logs ([installation](https://github.com/sgasser/pasteguard/blob/95df2a7a2732/docs/installation.mdx)) | None found in README |
| [Anwesha33/pii-shield](https://github.com/Anwesha33/pii-shield) (0, no licence file, Go, created 2026-09) | Yes ([README](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/README.md)) | Yes: hold-back of text that could still become `[[...]]` ([stream.go](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/internal/proxy/stream.go)) | In-memory, per request; README names a session store as the next step | Yes, but detector-only: held-out P 0.889 / R 1.000 on a synthetic corpus, detection latency p50 34 us / p99 87 us ([README](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/README.md)) [vendor-claim] |
| [huilangsh/closemask](https://github.com/huilangsh/closemask) (3, MIT, Go) | Yes | Yes: holds from `${` until a valid placeholder closes ([proxy.go](https://github.com/huilangsh/closemask/blob/2c771e14bfc2/internal/proxy/proxy.go)) | Layered memory + disk + Redis; Redis session uses `HSET` plus `EXPIRE`, plaintext values, no Lua ([redis_manager.go](https://github.com/huilangsh/closemask/blob/2c771e14bfc2/internal/session/redis_manager.go)) | None found |
| [CassiopeiaCode/CosyRedactGateway](https://github.com/CassiopeiaCode/CosyRedactGateway) (104, Apache-2.0, JS) | Yes (credential-focused) | Yes: Chat Completions, Responses, Anthropic Messages; test for every split position of a 75-byte placeholder ([stream.test.js L77](https://github.com/CassiopeiaCode/CosyRedactGateway/blob/07e0c603e078/test/stream.test.js), [README.en.md](https://github.com/CassiopeiaCode/CosyRedactGateway/blob/07e0c603e078/README.en.md)) | Stateless: only placeholders from the current request are restorable ([README.en.md](https://github.com/CassiopeiaCode/CosyRedactGateway/blob/07e0c603e078/README.en.md)) | Entropy calibration only (296 of 30,000 natural-word samples flagged) [vendor-claim] |
| [xiaYuTian11/maskit](https://github.com/xiaYuTian11/maskit) (314, AGPL-3.0, Python) | Yes | Yes: README claims split tokens are reassembled across chunks; split tests in [test_ext_stream_framing.py](https://github.com/xiaYuTian11/maskit/blob/b217a9b92641/tests/test_ext_stream_framing.py); does not rewrite Anthropic signed thinking blocks ([README](https://github.com/xiaYuTian11/maskit/blob/b217a9b92641/README_EN.md)) | Not determined | None found |
| [zolderio/token-proxy](https://github.com/zolderio/token-proxy) (31, Apache-2.0, Python) | Yes, Anthropic Messages only | Yes: "tail-buffer approach" ([README](https://github.com/zolderio/token-proxy/blob/8e64148f3077/README.md)) | Session-scoped deterministic pseudonyms; HMAC with per-session salt for donor IPs; sessions API ([README](https://github.com/zolderio/token-proxy/blob/8e64148f3077/README.md)) | None |
| [crp4222/PrivAiTe](https://github.com/crp4222/PrivAiTe) (54, BSD-3-Clause, Python) | Yes; fails closed on detection error | Yes, "streaming included", tool-call arguments too ([README](https://github.com/crp4222/PrivAiTe/blob/40ee53f559de/README.md)) | Mapping in memory for the request ([threat model](https://github.com/crp4222/PrivAiTe/blob/40ee53f559de/README.md)) | Recall and false positives on 120 ai4privacy 200k documents; compares itself with LiteLLM and LLM Guard [vendor-claim] |
| [ocultar-dev/ocultar](https://github.com/ocultar-dev/ocultar) (3, AGPL-3.0, Go) | Yes, deterministic tokens `[EMAIL_9c8f7a1b2d3e4f50]` | "real SSE streaming" proxy; restore-in-stream not verified | Encrypted DuckDB vault, AES-256-GCM with HKDF-SHA256, 90-day TTL ([README](https://github.com/ocultar-dev/ocultar/blob/b48d96782e69/README.md)) | Not checked |
| [daslabhq/pii-proxy](https://github.com/daslabhq/pii-proxy) (8, MIT) | Plausible fakes with a bijective map | Not verified | In-memory map | Own fine-tuned GLiNER: 91.5 F1 on Nemotron-PII held-out; 75.7 vs Presidio 42.0 F1 out-of-distribution [vendor-claim] ([README](https://github.com/daslabhq/pii-proxy/blob/183b057fc880/README.md)) |
| [packyme/privacy-filter](https://github.com/packyme/privacy-filter) (338, MIT, Go) | Redaction only; placeholders "are irreversible" ([README](https://github.com/packyme/privacy-filter/blob/64b8de3c2060/README.md)) | n/a | n/a | "millisecond latency" claim only |
| [zeroc00I/DontFeedTheAI](https://github.com/zeroc00I/DontFeedTheAI) (668, MIT, Python), [yurivenancio30/llm-dlp](https://github.com/yurivenancio30/llm-dlp) (5, MIT, Go) | Yes (pentest and Claude Code focused) | Not inspected | Per-engagement vault (DontFeedTheAI) | None found |

PrivAiTe also publishes its own comparison page of these projects ([comparison.md](https://github.com/crp4222/PrivAiTe/blob/40ee53f559de/docs/comparison.md)); I used it only as a lead list, not as evidence.

### 1.3 Things I saw in the source that a new implementation can avoid

All [src@sha], read not run:

- **pii-shield** uses one shared `pending` buffer for all `choices`, so `n>1` interleaving would cross-contaminate; it only rewrites `delta.content`, not `tool_calls[].function.arguments`; tail text held at the end is flushed as a synthetic delta after the `[DONE]` line is seen, which is after the chunk carrying `finish_reason` ([stream.go](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/internal/proxy/stream.go), [handler.go L252-L335](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/internal/proxy/handler.go)). It decodes the request into `map[string]any`, which turns numbers into float64 and re-orders keys ([encoding/json docs](https://pkg.go.dev/encoding/json)) and `json.Marshal` HTML-escapes `<`, `>`, `&` by default ([same docs](https://pkg.go.dev/encoding/json)).
- **closemask** holds any trailing `${` without a closing `}`, so literal `${` in code output can be held until a `}` appears or the stream ends ([proxy.go L1162-L1190](https://github.com/huilangsh/closemask/blob/2c771e14bfc2/internal/proxy/proxy.go)).
- **LiteLLM's** truncated-token fallback replaces a trailing partial token with the original value when the overlap is at least `min(20, len(token)//2)` characters ([L1304-L1327](https://github.com/BerriAI/litellm/blob/2d181e663373/litellm/proxy/guardrails/guardrail_hooks/presidio.py#L1304-L1327)); that is a heuristic restoration of text the model never completed.
- **LiteLLM** fails open when the Presidio analyzer errors in the default mask-only config (user-reported, open: [#30728](https://github.com/BerriAI/litellm/issues/30728)). PrivAiTe fails closed ([README](https://github.com/crp4222/PrivAiTe/blob/40ee53f559de/README.md)).
- **pii-shield** states a per-request vault is deliberate: "A process-wide vault would let a token minted for one tenant re-hydrate inside another tenant's response" ([README](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/README.md)). A shared Redis vault needs explicit session namespacing for the same reason.

### 1.4 Verdict on overlap

| Part of maskgate | Already covered by | Remaining gap |
|---|---|---|
| Mask request, restore response, typed tokens | PasteGuard, pii-shield, closemask, Cosy, maskit, PrivAiTe, LiteLLM (partly) | None |
| Boundary-safe SSE rehydration | Cosy (every split position tested), pii-shield, PasteGuard (per-channel), maskit | Per-choice + per-tool-call + finish ordering + mid-stream error handling, written down as a conformance suite |
| Redis session vault with TTL | closemask (HSET + EXPIRE, plaintext), Invisio sample (Redis), ocultar (encrypted DuckDB, not Redis) | Encrypted values, atomic get-or-create, namespacing by authenticated caller; thin |
| Regex tier with Luhn | pii-shield, privacy-filter, PrivAiTe via Presidio | Nothing unique |
| Presidio sidecar | LiteLLM (needs analyzer + anonymizer services), PrivAiTe (in-process) | A measured regex vs regex+Presidio comparison inside a proxy |
| Proxy-level overhead (p50/p99, TTFT delta, open-loop) | None found | **Open** |
| Per-entity P/R on a redistributable corpus + field-coverage matrix | PrivAiTe (recall on a licence-restricted sample, vendor-reported); pii-shield (synthetic, detector only) | **Open**, partly |

---

## 2. Streaming re-hydration

### 2.1 OpenAI chat-completions SSE format (primary: OpenAI's OpenAPI spec)

Source for this subsection: [openai-openapi `openapi.yaml`](https://github.com/openai/openai-openapi/blob/main/openapi.yaml) (schemas `CreateChatCompletionStreamResponse`, `ChatCompletionStreamResponseDelta`, `ChatCompletionStreamOptions`), fetched 2026-10-19. OpenAI's HTML API reference returned 403/404 to my fetch tool, so I used the spec that publishes the same schema.

- Each chunk is `chat.completion.chunk`; `id`, `created` are the same on every chunk; `choices[]` each carry `index`, `delta`, `finish_reason`, `logprobs`. "Can contain more than one elements if `n` is greater than 1." Interleaving across choices is therefore possible.
- `delta` fields: `content` (string or null), `tool_calls[]` (`ChatCompletionMessageToolCallChunk`: `index`, `id`, `function.name`, `function.arguments`), deprecated `function_call`, `refusal`, `role`, and `audio` (`transcript` text, base64 `data`). Tokens can appear in `content`, `refusal`, `tool_calls[].function.arguments`, `function_call.arguments` and `audio.transcript`.
- `function.arguments` is JSON "as generated by the model ... the model does not always generate valid JSON", streamed as fragments.
- `finish_reason`: `stop`, `length`, `tool_calls`, `content_filter`, `function_call`.
- With `stream_options.include_usage`, an extra chunk with `choices: []` and `usage` arrives before `data: [DONE]`; every other chunk has `usage: null`; "If the stream is interrupted, you may not receive the final usage chunk."
- End marker: `data: [DONE]` is "literal text, not a JSON chunk, and this frame has no `event: done` field". A failure after streaming has started may arrive as a data frame with an `error` object, and "An interrupted stream may end without the marker."
- `obfuscation`: "An obfuscation string added to normalize the size of streamed chunks as a mitigation to certain side-channel attacks", included by default, omitted when `stream_options.include_obfuscation` is false.
- The official Python SDK stops at `data: [DONE]` ([`_streaming.py` L71](https://github.com/openai/openai-python/blob/main/src/openai/_streaming.py)).
- SSE framing itself ([WHATWG HTML Standard, event stream parsing](https://html.spec.whatwg.org/multipage/server-sent-events.html#parsing-an-event-stream)): lines end with CRLF, LF or CR; UTF-8 only; lines starting with `:` are comments; multiple `data:` lines in one event are joined with LF; a blank line dispatches the event. A line-splitting parser that assumes `data: ` with a space and one data line per event is not spec-complete [analysis].

### 2.2 How existing tools handle a token split across deltas

| Tool | Approach | Gaps visible in source |
|---|---|---|
| LiteLLM (OpenAI format) | No split problem because nothing is emitted until the upstream stream ends ([L1639-L1712](https://github.com/BerriAI/litellm/blob/2d181e663373/litellm/proxy/guardrails/guardrail_hooks/presidio.py#L1639-L1712)) | First-token latency equals full generation time |
| LiteLLM (Anthropic raw SSE) | Per-delta `str.replace` on `content_block_delta`/`text_delta` ([L1586-L1613](https://github.com/BerriAI/litellm/blob/2d181e663373/litellm/proxy/guardrails/guardrail_hooks/presidio.py#L1586-L1613)) | A token split over two deltas is not restored; user report with output "mangled" when the hook is forced on: [#46087](https://github.com/BerriAI/litellm/issues/46087) |
| LLM Guard | Accumulate all output and re-run scanners ([example](https://github.com/protectai/llm-guard/blob/168c1034ffdb/examples/openai_streaming.py)) | Not a streaming rewriter; scanners run on the accumulated text |
| pii-shield (Go) | Hold back a trailing unclosed `[[` (cap 64 bytes) or a lone `[`; flush at `[DONE]`; tests all 32 split points of one token ([stream.go](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/internal/proxy/stream.go), [stream_test.go](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/internal/proxy/stream_test.go)) | Shared buffer across choices; `content` only; tail flushed after finish chunk |
| PasteGuard (TS) | `StreamRestorer`; separate restorer per (choice, tool-call) for arguments; escapes restored values for JSON string context ([stream-transformer.ts](https://github.com/sgasser/pasteguard/blob/95df2a7a2732/src/providers/openai/stream-transformer.ts)) | Not inspected beyond this file |
| CosyRedactGateway (JS) | Hold a trailing prefix until it is a complete known placeholder or can no longer become one ([REFERENCE.md](https://github.com/CassiopeiaCode/CosyRedactGateway/blob/07e0c603e078/docs/REFERENCE.md)); exhaustive split test ([stream.test.js L77](https://github.com/CassiopeiaCode/CosyRedactGateway/blob/07e0c603e078/test/stream.test.js)) | Credential-focused |
| closemask (Go) | Hold from the last `${` until closed and valid ([proxy.go](https://github.com/huilangsh/closemask/blob/2c771e14bfc2/internal/proxy/proxy.go)) | Unbounded hold on literal `${` |
| token-proxy, maskit | "tail-buffer" / split tests ([token-proxy README](https://github.com/zolderio/token-proxy/blob/8e64148f3077/README.md), [maskit tests](https://github.com/xiaYuTian11/maskit/blob/b217a9b92641/tests/test_ext_stream_framing.py)) | Not inspected in depth |

### 2.3 Design options for a boundary-safe rehydrator

All options need one rewriter state per **(choice index, channel)** where channel is `content`, `refusal`, `tool_calls[i].function.arguments`, `function_call.arguments`, `audio.transcript` [analysis from the schema in 2.1].

| Option | Mechanism | First-token impact | Correctness risks |
|---|---|---|---|
| A. Viable-prefix hold-back (recommended) | Keep the longest suffix that is still a prefix of some live token string; emit everything before it. Token grammar is small (`<`, type from a fixed set, `_`, hex, `>`), so a tiny DFA decides "could still become a token" per character | None unless the first characters are a viable prefix; ordinary prose resolves in one character | Must flush on: `finish_reason` for that choice (before forwarding the finish chunk), `[DONE]`, error frame, upstream EOF |
| B. Hold from last `<` to a length cap (what most repos do) | Simpler; hold if trailing text has an unclosed opener, up to `maxTokenLen` | Delays any text containing `<` (HTML, XML, generics, comparisons) by up to a cap | Stalls if the cap is wrong; closemask-style unbounded hold |
| C. Sentence- or line-level buffering | Flush at newline or sentence end | Adds up to a sentence of latency | Splits are still possible if a token spans a boundary |
| D. Buffer-all (LiteLLM OpenAI path) | Assemble, rewrite, re-emit | First-token latency = full generation | Simplest; destroys the point of streaming |

Details that tend to be missed [analysis unless cited]:

- **Tool-call arguments are JSON inside a JSON string.** The model's `arguments` string is itself JSON text. A restored value containing `"`, `\`, newline or non-ASCII must be escaped for the inner JSON, then again by the outer chunk encoder. PasteGuard does the inner escape explicitly ([stream-transformer.ts](https://github.com/sgasser/pasteguard/blob/95df2a7a2732/src/providers/openai/stream-transformer.ts)). Any component that re-serializes the inner arguments JSON with an HTML-escaping encoder (Go's `encoding/json` does this by default for `<`, `>`, `&`, [docs](https://pkg.go.dev/encoding/json)) can replace each angle bracket of a token with a six-character Unicode escape (backslash, `u`, then four hex digits) inside the arguments string, so the text the rewriter sees no longer equals the vault key, and a model may also emit such escapes itself [analysis]. Pick a token alphabet no common JSON or HTML encoder rewrites (letters, digits, underscore, brackets that are not HTML-escaped) so the matcher only has to deal with one form.
- **`n>1`**: key all buffers by `choices[].index`; frames can interleave.
- **Parallel tool calls**: key by `tool_calls[].index` as well.
- **Usage chunk** has `choices: []`: pass through untouched. **`[DONE]`** is not JSON: flush all buffers, emit synthetic delta chunks that copy `id`, `created`, `model`, then forward `[DONE]`.
- **Chunk-boundary vs event-boundary**: TCP reads can split SSE lines and UTF-8 sequences; parse events (blank-line terminated, per the WHATWG rules above) before touching JSON.
- **Rewriting changes lengths**, which defeats the upstream `obfuscation` padding for anyone observing the proxy-to-app hop (section 8).

### 2.4 Latency cost of the hold-back

[analysis] With option A the extra delay is bounded by the longest token (for `<CREDIT_CARD_7f3a8c21>` about 22 characters, i.e. a handful of upstream tokens) and applies only to text that is a viable token prefix. Measure it as the distribution of (downstream event time minus upstream event time) per event, plus time-to-first-token and total-time deltas against a mock upstream with fixed chunk cadence (section 7). Option B should be benchmarked against A on HTML-heavy output to show the difference.

### 2.5 Failure modes to design for and to count in the benchmark

- Model lowercases, translates, paraphrases or drops a token. LLM Guard ships case-insensitive and fuzzy matching ([deanonymize.py](https://github.com/protectai/llm-guard/blob/168c1034ffdb/llm_guard/output_scanners/deanonymize.py)); PrivAiTe states "A model inventing a value instead of copying a placeholder: nothing can be restored then" ([README](https://github.com/crp4222/PrivAiTe/blob/40ee53f559de/README.md)). Fuzzy matching raises false-restoration risk [analysis].
- Stream ends mid-token because of `max_tokens` (`finish_reason: length`): flush the partial text as-is; LiteLLM instead restores a truncated suffix by heuristic ([L1304-L1327](https://github.com/BerriAI/litellm/blob/2d181e663373/litellm/proxy/guardrails/guardrail_hooks/presidio.py#L1304-L1327)), which can restore a value the model never emitted.
- Unknown token (model invents `<EMAIL_ffff>`): leave visible and count it (pii-shield leaves it visible, [README](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/README.md)).
- Token appears inside a URL, code block or markdown image: see the exfiltration row in section 8.
- Upstream closes early or sends an error frame: flush, then forward the error.

### 2.6 "Any LLM API" is three SSE dialects, not one

- Anthropic Messages: `event:` + `data:` pairs; `message_start`, `content_block_start`, `content_block_delta` (`text_delta`, `input_json_delta` with `partial_json` fragments, `thinking_delta`, `signature_delta`), `content_block_stop`, `message_delta`, `message_stop`, plus `ping` and `error` events ([Anthropic streaming docs](https://platform.claude.com/docs/en/build-with-claude/streaming)). Tool input arrives as partial JSON strings, same double-encoding issue. A `signature_delta` verifies the thinking block's integrity, so rewriting thinking text is unsafe; maskit restores only visible text and tool arguments and leaves signed thinking blocks alone ([README](https://github.com/xiaYuTian11/maskit/blob/b217a9b92641/README_EN.md)).
- OpenAI Responses API: separate event types; LiteLLM has open bugs where `/v1/responses` input is not masked ([#30728](https://github.com/BerriAI/litellm/issues/30728)). PasteGuard and Cosy implement it.
- OpenAI-compatible servers (vLLM, Ollama and similar) mostly follow chat-completions; I did not verify their deviations (UNVERIFIED).

Recommendation: claim "OpenAI chat-completions-compatible" in the README, ship one adapter, put the rest in "not supported".

---

## 3. Detection pitfalls (regex tier)

### 3.1 Luhn and card numbers

- Luhn is a single appended check digit ([US 2,950,048](https://patents.google.com/patent/US2950048A/en): "a single digit, called the check digit, is appended to the righthand end of the original or true number"). Because the check digit is one of 10 values, an arbitrary digit string passes about 1 time in 10. **[my check]** 200,000 random digit strings per length, seed 1: pass rate 0.0994 (13 digits), 0.1010 (14), 0.1005 (15), 0.1008 (16), 0.1003 (17), 0.1000 (18), 0.1000 (19). pii-shield independently reports an 18-digit tracking number passing Luhn and added issuer-prefix and length checks ([README](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/README.md)).
- Typical false-positive sources (13-19 digit runs): order and tracking numbers, 13-digit millisecond epoch timestamps, long numeric IDs, concatenated phone numbers [analysis].
- Presidio's `CreditCardRecognizer` uses a weak regex (score 0.3) plus Luhn, strips only `-` and space before checking, and a leading-digit set of 1, 3, 4, 5[0-5], 6 ([credit_card_recognizer.py@4f0e236](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/predefined_recognizers/generic/credit_card_recognizer.py)). **[my check]** running that regex on test strings: matches 13-17 digit runs, Amex written 4-6-5 with spaces, Diners 4-6-4 with spaces, Visa with dashes or spaces; **no match** for Visa with dot separators, for a Mastercard 2-series number (`2221...`), or for 18- and 19-digit PANs. Presidio's own entity table says CREDIT_CARD is "between 12 to 19 digits" ([supported_entities.md](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/supported_entities.md)), so doc and regex disagree at this commit.
- Issuer (IIN/BIN) and length validation per ISO/IEC 7812-1: I could not fetch the ISO pages (403), so exact current IIN ranges and PAN lengths are UNVERIFIED from a primary source. pii-shield validates issuer prefix and length ([README](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/README.md)). Reversible masking lowers the cost of a false positive (the value is restored in output) but a masked order ID still hurts the model's task [analysis].
- Separators: accept space, dash, and optionally dot; require consistent separator and plausible grouping; strip before Luhn; keep original span for replacement [analysis].

### 3.2 SSN

- SSA's POMS lists these as invalid SSNs: area "000", "666", or in the 900 series; group "00"; serial "0000" ([POMS RM 10201.035, 06/23/2011](https://secure.ssa.gov/poms.nsf/lnx/0110201035)). The SSA randomization pages returned 403 to my fetcher; the 2011 randomization date is from search-result summaries only (UNVERIFIED as a direct quote).
- Presidio's `UsSsnRecognizer`: five patterns scored 0.05 (four "very weak" shapes including bare 9 digits) and 0.5 (`ddd[- .]dd[- .]dddd`); context words (social, security, ssn, ssid) lift scores; `invalidate_result` rejects mixed delimiters, all-same digit, group `00`, serial `0000`, area `000` and `666`, and three published sample numbers (123456789, 987654320, 078051120) ([us_ssn_recognizer.py@4f0e236](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/predefined_recognizers/country_specific/us/us_ssn_recognizer.py)). It does **not** reject area 900-999. Context boost: factor 0.35, minimum 0.4 ([lemma_context_aware_enhancer.py](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/context_aware_enhancers/lemma_context_aware_enhancer.py)); default analyzer threshold 0 ([default_analyzer.yaml](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/conf/default_analyzer.yaml)).
- Do not use the SSA invalid ranges to skip masking: Presidio's US_ITIN is "nine digits that start with a 9 and contain a 7 or 8 as the 4th digit" ([supported_entities.md](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/supported_entities.md)), so 9xx-shaped numbers can be real taxpayer IDs. Use structure only to lower confidence, and require a context word or dashes for bare 9-digit strings [analysis].
- Size of the valid-structure space (matters for hashing, section 5): 898 areas (001-899 minus 666) x 99 groups x 9,999 serials = 888,931,098 **[my check, arithmetic from the POMS rules]**.

### 3.3 Phone numbers

- libphonenumber is the reference library (Java, C++, JavaScript) with parsing, formatting, validation and `findNumbers`; `isValidNumber` uses "length and prefix information", `isPossibleNumber` only length; the Go port by nyaruka is third-party ([libphonenumber README](https://github.com/google/libphonenumber)). nyaruka/phonenumbers is MIT, latest release v1.8.1 on 2026-07-15, and exposes `FindNumbers(text, defaultRegion)` using `VALID` leniency by default ([releases](https://github.com/nyaruka/phonenumbers), [phonenumberutil.go](https://github.com/nyaruka/phonenumbers/blob/main/phonenumberutil.go)).
- Presidio's `PhoneRecognizer` runs python-phonenumbers' `PhoneNumberMatcher` over default regions US, GB, DE, FR, IL, IN, CA, BR at leniency 1, fixed score 0.4 ([phone_recognizer.py@4f0e236](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/predefined_recognizers/generic/phone_recognizer.py)). One full-text pass per region, so cost scales with region count [analysis].
- A bare regex has no checksum to separate a 10-digit mobile from a 10-digit employee ID; pii-shield's held-out false positives include exactly that (`9988776655` flagged as an Indian mobile) ([README](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/README.md)).
- Practical rule [analysis]: regex candidate finder for E.164 and common national formats, then validate with a libphonenumber port when a default region is configured; otherwise require a `+` prefix or a context word.

### 3.4 Email

- RFC 5322 `addr-spec = local-part "@" domain`, with `local-part = dot-atom / quoted-string / obs-local-part` and `domain = dot-atom / domain-literal / obs-domain` ([RFC 5322 section 3.4.1](https://www.rfc-editor.org/rfc/rfc5322#section-3.4.1)). The WHATWG HTML Standard defines a "valid email address" regex and calls it "a willful violation of RFC 5322" ([spec](https://html.spec.whatwg.org/multipage/input.html#valid-e-mail-address)). Go's `net/mail` implements RFC 5322 plus RFC 6532 (UTF-8) but not obsolete syntax or full CFWS ([docs](https://pkg.go.dev/net/mail)).
- Presidio's regex allows a broad local-part character set and requires at least one dot in the domain, then validates with `tldextract` (`result.fqdn != ""`) ([email_recognizer.py@4f0e236](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/predefined_recognizers/generic/email_recognizer.py)).
- Edge cases to test: plus-addressing, quoted local parts with spaces, trailing punctuation (`john@acme.com.`), `mailto:` and URLs, Unicode local parts and IDN domains (note Go RE2's `\b` and `\w` are ASCII-only, [RE2 syntax](https://github.com/google/re2/wiki/Syntax)), obfuscated forms such as `[AT]`/`[DOT]` (LLM Guard lists these as supported, [anonymize.md](https://github.com/protectai/llm-guard/blob/168c1034ffdb/docs/input_scanners/anonymize.md); token-proxy lists obfuscated emails as a blind spot, [README](https://github.com/zolderio/token-proxy/blob/8e64148f3077/README.md)).

### 3.5 IBAN and other checksum types worth adding

- IBAN: country code, 2 check digits, BBAN; validate by mod-97 plus per-country length/format. I could not fetch ISO 13616 or the SWIFT registry (403), so the standard itself is UNVERIFIED here; Presidio computes check digits and also validates format against per-country patterns ([iban_recognizer.py@4f0e236](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/predefined_recognizers/generic/iban_recognizer.py)). Cheap and low false-positive, good second checksum entity after cards.
- Other checksum or structure recognizers already present in Presidio, useful as a reference list: UK NHS (checksum), Australian ABN (modulus 89) and ACN, crypto addresses, US NPI ([supported_entities.md](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/supported_entities.md), [au_abn_recognizer.py](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/predefined_recognizers/country_specific/australia/au_abn_recognizer.py)). Canadian SIN is Luhn-validated per Bedrock's entity description ([docs](https://docs.aws.amazon.com/bedrock/latest/userguide/guardrails-sensitive-filters.html)). For the MVP add IBAN only.

### 3.6 What Go's regexp costs vs Java and Python

| Topic | Go `regexp` | Java `java.util.regex` | Python |
|---|---|---|---|
| Engine guarantee | "guaranteed to run in time linear in the size of the input" ([pkg.go.dev/regexp](https://pkg.go.dev/regexp)) | "traditional NFA-based matching with ordered alternation as occurs in Perl 5"; no linear-time claim on the page ([Pattern javadoc](https://docs.oracle.com/en/java/javase/21/docs/api/java.base/java/util/regex/Pattern.html)) | Presidio uses the third-party `regex` module with a timeout (default 60 s via `REGEX_TIMEOUT_SECONDS`) for IBAN ([iban_recognizer.py](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/predefined_recognizers/generic/iban_recognizer.py)) |
| Lookahead/lookbehind | Not supported: `(?=re)`, `(?!re)`, `(?<=re)`, `(?<!re)` all "NOT SUPPORTED" ([RE2 syntax](https://github.com/google/re2/wiki/Syntax)) | Supported ([Pattern javadoc](https://docs.oracle.com/en/java/javase/21/docs/api/java.base/java/util/regex/Pattern.html)) | Supported in practice: Presidio's IBAN and card patterns use lookaround ([iban_recognizer.py](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/predefined_recognizers/generic/iban_recognizer.py), [credit_card_recognizer.py](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/predefined_recognizers/generic/credit_card_recognizer.py)) |
| Backreferences, possessive, atomic groups | Not supported ([RE2 syntax](https://github.com/google/re2/wiki/Syntax)) | Supported ([Pattern javadoc](https://docs.oracle.com/en/java/javase/21/docs/api/java.base/java/util/regex/Pattern.html)) | Not checked against Python's `re` docs (UNVERIFIED); Presidio's recognizers use lookaround and are written for Python |
| `\b`, `\w`, `\d` | ASCII-only ([RE2 syntax](https://github.com/google/re2/wiki/Syntax)) | Configurable | Unicode by default |
| Match semantics | Leftmost-first by default; `Longest()` for leftmost-longest ([pkg.go.dev/regexp](https://pkg.go.dev/regexp)) | Ordered alternation | Ordered alternation |
| Linear-time option | Built in | RE2/J, "not a drop-in replacement", "not an official Google product" ([README](https://github.com/google/re2j)) | n/a |
| Speed | Open work items: [DFA port #11646](https://github.com/golang/go/issues/11646), [perf #26623](https://github.com/golang/go/issues/26623); a community benchmark pasted in #26623 shows Go slower than Java and Python 3 on three patterns (not an official benchmark) | JIT-compiled | Interpreted |

What the RE2 limits cost in practice [analysis]: Presidio's own patterns use lookaround (IBAN: `(?<![A-Z0-9])...(?![A-Z0-9])`; card: `(?!1\d{12}(?!\d))`), so in Go each becomes "broad candidate regex, then check the bytes before and after the match, then validate with code". That is the same pipeline you need anyway for Luhn, mod-97 and context words. For prompts of a few KB, regex time is unlikely to matter next to network time, but measure it (an item for the benchmark).

---

## 4. Presidio facts

All from the repo at [data-privacy-stack/presidio@4f0e236](https://github.com/data-privacy-stack/presidio/tree/4f0e236e31a5) (2026-10-19) unless linked otherwise.

- **Ownership and distribution.** The project moved from Microsoft to the community org Data Privacy Stack; MIT licence continues ([project_transition.md](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/project_transition.md)). Docs site is now `presidio.dataprivacystack.org`: `microsoft.github.io/presidio` shows a "This page has moved" notice and `data-privacy-stack.github.io/presidio` answers 301 to the new host (both observed by my fetch tool on 2026-10-19); `github.com/microsoft/presidio` resolves to `data-privacy-stack/presidio` in the GitHub API. Container images: `ghcr.io/data-privacy-stack/presidio-analyzer` and `...-anonymizer`; "The legacy Microsoft Container Registry images at `mcr.microsoft.com/presidio-*` are no longer updated" ([installation.md](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/installation.md)).
- **Analyzer REST API** ([app.py](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/app.py), [analyzer_request.py](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/analyzer_request.py)): Flask app; `POST /analyze`, `GET /recognizers?language=`, `GET /supportedentities?language=`, `GET /health`. `/analyze` body: `text` (string, or a list for batch), `language` (required), `entities`, `correlation_id`, `score_threshold`, `return_decision_process`, `ad_hoc_recognizers`, `context`, `allow_list`, `allow_list_match`, `regex_flags`. Response is a list of results (`entity_type`, `start`, `end`, `score`, optional `analysis_explanation`); `recognition_metadata` is stripped. Default `score_threshold` is 0 ([default_analyzer.yaml](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/conf/default_analyzer.yaml)); default language list is `en`. Entity list: [supported_entities.md](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/supported_entities.md).
- **Default NLP engine:** spaCy, model `en_core_web_lg`, with `ORGANIZATION` and several numeric labels ignored ("Has many false positives") ([default.yaml](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/presidio_analyzer/conf/default.yaml)). spaCy 3.8.0 wheel sizes: sm 12 MiB, md 31 MiB, lg 382 MiB, trf 436 MiB; `en_core_web_lg` NER on its OntoNotes test set: P 85.21, R 85.87, F 85.54 ([spacy-models releases](https://github.com/explosion/spacy-models/releases/tag/en_core_web_lg-3.8.0), sizes via the GitHub release assets API). Those labels are the 18 OntoNotes classes, not PII classes. Alternative configs ship in the repo: `slim` (en_core_web_sm, GLiNER recognizer), `no_op` NLP engine, `transformers`, `stanza` ([conf/](https://github.com/data-privacy-stack/presidio/tree/4f0e236e31a5/presidio-analyzer/presidio_analyzer/conf)).
- **Docker image** ([Dockerfile](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/Dockerfile), [entrypoint.sh](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-analyzer/entrypoint.sh)): `python:3.12-slim` base, models downloaded at build from the NLP config, port 3000, non-root UID 1001, health check on `/health`, `gunicorn -w "$WORKERS"` with `WORKERS=1` by default; batch tuning env vars `BATCH_SIZE` (default 500) and `N_PROCESS` (default 1) in `app.py`. **No authentication by design**; the FAQ says not to expose it to untrusted networks ([faq.md](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/faq.md)).
- **Documented performance guidance:** only "Anything above 100ms per request with 100 tokens is probably not good enough" for custom recognizers ([developing_recognizers.md](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/analyzer/developing_recognizers.md)). GPU: `PRESIDIO_DEVICE` env var ([gpu_usage.md](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/analyzer/nlp_engines/gpu_usage.md)). I found **no published latency, throughput or memory-footprint numbers** (UNVERIFIED; measure it).
- **Accuracy limits (Presidio's own material):** "Notebook 4 ... this is using the vanilla Presidio, and the results aren't very accurate"; Notebook 5 "boost[s] the f score in ~30%" ([evaluation index](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/evaluation/index.md)). The committed notebook outputs on presidio-research's synthetic template data, binary PII-vs-O level: vanilla `en_core_web_lg` config **F2 0.661, precision 0.733, recall 0.646**; customised config **F2 0.91, P 0.921, R 0.907** ([notebook 4](https://github.com/data-privacy-stack/presidio-research/blob/7f568b6da8a5/notebooks/4_Evaluate_Presidio_Analyzer.ipynb), [notebook 5](https://github.com/data-privacy-stack/presidio-research/blob/7f568b6da8a5/notebooks/5_Evaluate_Custom_Presidio_Analyzer.ipynb)). Frequent notebook-4 false positives: `8` (in "8 years") and "sunday labor" as DATE_TIME, "Greek" as DEMOGRAPHIC, nationalities like "Greenlander" as PERSON or LOCATION. Treat these as illustrative: the data is template-generated, not real prompts.
- **Third-party numbers (competitors, not reproduced):** PrivAiTe reports its "light" preset (full Presidio) at 62.7% span recall on 120 ai4privacy 200k documents ([README](https://github.com/crp4222/PrivAiTe/blob/40ee53f559de/README.md)); pii-proxy reports Presidio at 42.0 F1 out-of-distribution ([README](https://github.com/daslabhq/pii-proxy/blob/183b057fc880/README.md)). [vendor-claim]
- **Sidecar notes:** LiteLLM's Presidio setup calls two services (analyzer :5002, anonymizer :5001 in the sample) ([litellm.md](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/samples/docker/litellm.md)); maskgate would only need `/analyze` and can do replacement locally, one hop instead of two [analysis]. Default single worker means a sidecar saturates one core unless `WORKERS` is raised [analysis from the entrypoint]. Name and address detection is context-dependent NER; short or fragmentary prompts give it less context [analysis; no primary measurement found].
- **Anonymizer operators** ([anonymizer docs](https://presidio.dataprivacystack.org/anonymizer/)): replace, redact, hash, mask, encrypt, custom, keep. `hash` uses sha256/sha512 with a random 32-byte salt per entity unless a salt is given ([hash.py](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-anonymizer/presidio_anonymizer/operators/hash.py)), so Presidio's default hash is not a stable pseudonym.

---

## 5. Format-preserving and other token styles

### 5.1 NIST FPE status (as of 2026-10-19)

- Original SP 800-38G (March 2016) specified FF1 and FF3; its CSRC page is marked "Withdrawn on August 04, 2016. Superseded by" an update ([CSRC](https://csrc.nist.gov/pubs/sp/800/38/g/final)).
- SP 800-38G Rev. 1 initial public draft (Feb 2019) revised FF3 into FF3-1 (56-bit tweak) and set a minimum domain size of one million for FF1 and FF3-1 ([IPD](https://csrc.nist.gov/pubs/sp/800/38/g/r1/ipd)); that page is itself marked "Obsoleted on February 03, 2025 by ... 2nd Public Draft".
- Rev. 1 second public draft (2025-02-03): "The encryption method FF3 is no longer specified"; "Beyne described a weakness in the tweak schedule that affected both FF3 and FF3-1 but not FF1"; FF1 remains, with a larger domain size ([2PD](https://csrc.nist.gov/pubs/sp/800/38/g/r1/2pd)). I found no final Rev. 1 as of today (UNVERIFIED that none exists; I only searched CSRC and NIST pages).
- Consequence: if FPE is used at all, FF1 only; do not use FF3 or FF3-1. For fixed-alphabet fields (9-digit SSN, 16-digit PAN) FF1 fits the domain-size floor; emails and names are not fixed-alphabet domains and need an encoding layer [analysis].

### 5.2 Token style options

| Style | Pros | Cons |
|---|---|---|
| Opaque typed token `<EMAIL_7f3a>` | Unmistakable, never collides with real data in practice, trivial to match, can carry type | May look like markup (`<...>`); JSON/HTML escaping issues (section 2.3); model may treat it as a tag |
| Counter token `<EMAIL_1>` (LiteLLM, PasteGuard) | Short, readable, collision-free per type | Needs atomic counter; guessable across sessions |
| Plausible fake (Faker or pii-proxy) | Keeps fluency | Fake can collide with a real value in the conversation, which breaks string-replace restoration; Faker seeding is only reproducible for the same Faker version, `.unique` can raise `UniquenessException`, and the docs warn "collisions are more likely than you'd think" ([Faker docs](https://faker.readthedocs.io/en/master/)); needs a bijective map and collision handling (pii-proxy describes one, [README](https://github.com/daslabhq/pii-proxy/blob/183b057fc880/README.md)) |
| FF1 ciphertext | No vault needed for restore; same shape as input | Indistinguishable from real data (collision/ambiguity), key management, deterministic equality leak, domain-size floor, not defined for emails/names |
| Presidio `encrypt` | Reversible with the key | AES-CBC with random IV, base64url output, no integrity tag, long and not format-preserving ([aes_cipher.py](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-anonymizer/presidio_anonymizer/operators/aes_cipher.py)) |

### 5.3 Does placeholder style change LLM task quality? No clean primary measurement found

- Deußer et al., "On the Impact of Anonymization on the Performance of Large Language Models" (arXiv 2609.11335, Sept 2026): 11 benchmarks, 5 models; anonymization generally degrades performance with variation by model and task; per the abstract page, reversible entity-preserving anonymization outperforms irreversible redaction; telling the model about the anonymization gave no noticeable benefit; the abstract page does not state the placeholder style ([abs](https://arxiv.org/abs/2609.11335)).
- SurrogateShield (arXiv 2606.29567, single author, June 2026): surrogate substitution vs placeholder redaction on a 1,124-query corpus; BERTScore 81.59 to 94.85; self-reported ([abs](https://arxiv.org/abs/2606.29567)). It compares redaction with surrogates, not opaque-unique tokens with surrogates.
- pii-proxy's README asserts that opaque `PERSON_1`-style tokens "degrade fluency" ([README](https://github.com/daslabhq/pii-proxy/blob/183b057fc880/README.md)); I did not find an experiment in that repo that tests it (grep of its `experiments/` for the claim found none).
- Conclusion: **no primary source I found measures opaque unique tokens against plausible fakes on the same tasks.** Do not claim either is better. If you want a claim, it needs its own A/B with a real model, which is outside the 3-week budget.

### 5.4 Token ID design: random vs HMAC-of-value

| Property | Random ID (plus value-to-token index) | HMAC(key, session, type, value) truncated |
|---|---|---|
| Same value, same token within a session | Needs a value-to-token index in the vault | Free, no lookup |
| Leaks anything about the value | No | Equality only (if the key is secret) |
| Offline dictionary attack on low-entropy values | Not applicable | Yes if the key leaks or is guessable: SSN valid-structure space is 888,931,098 [my check], phone and card spaces are also small; an unkeyed hash is trivially enumerable |
| Collisions | Check at allocation | Truncation collides: with 4 hex digits (16 bits) the birthday bound gives about 7% collision chance at 100 distinct values of one type and about 39% at 256 **[my check]**; so still check the vault, or use at least 8 hex digits |
| Cross-session linkage | None | None only if the session id is in the HMAC input |
| Key handling | None | Keys should be random or from a strong PRNG and refreshed ([RFC 2104](https://www.rfc-editor.org/rfc/rfc2104)) |

Recommendation [analysis]: random ID of at least 8 hex digits plus a value-index keyed by HMAC(index_key, type||value) (not by the plaintext value) so Redis never holds plaintext as a field name. A "hash" policy for SSNs/phones that stores or emits `sha256(value)` is reversible by enumeration; if you offer it, make it HMAC with a per-deployment secret and say so in the README, or drop it.

---

## 6. Redis vault

### 6.1 Is Lua needed for atomic allocation?

The operation is "get existing token for (session, type, value), else allocate one and write both directions with a TTL".

| Approach | How | Atomic? | Round trips | Notes (sources) |
|---|---|---|---|---|
| `SET ... NX GET EX` (no Lua) | `SET v2t:{sid}:<hmac> <candidate> NX GET EX <ttl>` returns the existing token or null; per the command's return rules `GET` returns the previous value whether or not the key was set ([SET](https://redis.io/docs/latest/commands/set/)). Write `t2v` first with `NX` (retry on collision), then `v2t`; if `v2t` already held a token, use it and let the orphan `t2v` expire | Each step atomic; no state is left that points nowhere; concurrent requests converge on one token | 2 | Works for random tokens. `SETNX`/`GETSET` are noted as replaceable by `SET` options ([SET](https://redis.io/docs/latest/commands/set/)) |
| `MULTI`/`EXEC` | Queue commands | Isolated, but commands are queued and run at `EXEC`, so you cannot branch on a prior read; `WATCH` gives optimistic CAS ([transactions](https://redis.io/docs/latest/develop/using-commands/transactions/)) | 1-2 | Not enough for get-or-create without retries |
| Lua `EVAL`/`EVALSHA` | One script does `HGET v2t`, else `INCR counter`/`HSET` both, `EXPIRE` | "Redis guarantees the script's atomic execution"; server blocked while it runs ([EVAL intro](https://redis.io/docs/latest/develop/programmability/eval-intro/)) | 1 | Script cache is volatile (`NOSCRIPT` after restart/failover), so use `EVALSHA` with a load-and-retry fallback; go-redis' `Script` helper does this ([script.go](https://github.com/redis/go-redis/blob/master/script.go)); all touched keys must be passed as `KEYS` ([EVAL intro](https://redis.io/docs/latest/develop/programmability/eval-intro/)) |
| Redis Functions (7.0+) | `FUNCTION LOAD` a library, `FCALL` | Atomic like scripts; persisted and replicated ([Functions](https://redis.io/docs/latest/develop/programmability/functions-intro/)) | 1 | In Cluster you must load on every node yourself ([Functions](https://redis.io/docs/latest/develop/programmability/functions-intro/)); more ops than a single compose service needs |

Verdict [analysis]: Lua is **not required** for random tokens; it is justified for (a) per-type sequential counters (`INCR` plus `HSETNX` must be atomic) and (b) saving one round trip. "Lua vs no-Lua allocation" is a good benchmark axis (p50/p99 and allocations per second) rather than an assumption. If you use scripts, keep them tiny: they block the server.

### 6.2 Session-scoped key design [analysis]

- Per session two hashes sharing a hash tag: `mg:{<sid>}:t2v` (token to AES-GCM ciphertext) and `mg:{<sid>}:v2t` (keyed hash of value to token); one `EXPIRE` on both, refreshed on each request (sliding) with an absolute cap. Per-field TTL exists as `HEXPIRE` since Redis 7.4.0 ([HEXPIRE](https://redis.io/docs/latest/commands/hexpire/)) but one key TTL is simpler and makes "delete session" a single `DEL`.
- `SET` without `KEEPTTL` discards the previous TTL ([SET](https://redis.io/docs/latest/commands/set/)); be explicit about TTL on every write.
- The `sid` must not be caller-chosen alone: derive it from the authenticated API key plus an optional caller session header (for example HMAC of both), otherwise anyone who guesses a session id can rehydrate its tokens. Without a session header, use a per-request vault.
- Rehydration looks up only tokens in the current session's `t2v`; an unknown token stays visible (cross-session token injection cannot restore anything).

### 6.3 Encryption, ACL, TLS

- Values: AES-256-GCM per value with random 96-bit nonce, associated data = session id + token (binds ciphertext to its slot), key id prefix for rotation; key held by the proxy (env or KMS), not in Redis [analysis]. Do not copy Presidio's AES-CBC without integrity ([aes_cipher.py](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/presidio-anonymizer/presidio_anonymizer/operators/aes_cipher.py)).
- Redis TLS: supported from version 6, optional, enabled with `tls-port` and cert/key/CA files; when enabled "Redis uses mutual TLS and requires clients to authenticate with a valid certificate" unless `tls-auth-clients no`; TLS lowers per-instance throughput ([TLS docs](https://redis.io/docs/latest/operate/oss_and_stack/management/security/encryption/)).
- ACL: the default user is `on nopass ~* &* +@all`, i.e. any connection can run any command ([ACL docs](https://redis.io/docs/latest/operate/oss_and_stack/management/security/acl/)). Create a dedicated user limited to the commands you use and the `mg:*` key pattern. Key patterns do not limit commands that take no keys: `FLUSHALL`, `FLUSHDB`, `SWAPDB` need explicit `-flushall -flushdb -swapdb` ([ACL docs](https://redis.io/docs/latest/operate/oss_and_stack/management/security/acl/)).
- Redis OSS encryption at rest and persistence settings for a vault: I did not find or fetch a statement; treat as UNVERIFIED. Practical stance [analysis]: disable RDB/AOF for the vault (tokens are disposable), and rely on proxy-side encryption anyway.
- Failure semantics [analysis]: vault write fails then reject the request (fail closed, 5xx); vault read miss on rehydration then leave the token visible and increment a counter; never forward unmasked on error (LiteLLM issue [#30728](https://github.com/BerriAI/litellm/issues/30728) shows the failure mode).

---

## 7. Benchmark datasets and method

### 7.1 Datasets and licences (dataset cards read 2026-10-19)

| Dataset | What it is | Licence terms on the card | Redistribute in a repo? |
|---|---|---|---|
| [nvidia/Nemotron-PII](https://huggingface.co/datasets/nvidia/Nemotron-PII) | 100,000 English synthetic, persona-grounded records, 55+ PII/PHI categories, span annotations, 50k train / 50k test, structured and free text | CC BY 4.0 ("ready for commercial use") | Yes with attribution; but avoid vendoring 100k records, download in the harness |
| [gretelai/gretel-pii-masking-en-v1](https://huggingface.co/datasets/gretelai/gretel-pii-masking-en-v1) | Synthetic English documents with PII/PHI spans; 50k/5k/5k | Apache-2.0 (card metadata) | Yes |
| [ai4privacy/pii-masking-openpii-1.5m](https://huggingface.co/datasets/ai4privacy/pii-masking-openpii-1.5m) | 1,636,375 synthetic examples, 19 labels (incl. EMAIL, TELEPHONENUM, SOCIALNUM, CREDITCARDNUMBER, GIVENNAME, SURNAME, STREET, CITY, ZIPCODE), 30 languages | Card text: CC-BY-4.0, "synthetic PII only"; HF header reads `license: other` with `license_name: cc-by-4.0` (inconsistent metadata) | Probably yes with attribution; check before copying |
| [ai4privacy/openpii-masking-mini-10k](https://huggingface.co/datasets/ai4privacy/openpii-masking-mini-10k) | 10,000-sample stratified subset of openpii-1m; fields `source_text`, `privacy_mask` (label, start, end, value) | `license: cc-by-4.0` in header | Yes with attribution; the best small multilingual option |
| [ai4privacy/pii-masking-200k](https://huggingface.co/datasets/ai4privacy/pii-masking-200k) | en, fr, de, it; 54 PII classes; "Synthetic data generated using proprietary algorithms" | Repo `license.md`: free for individuals, for-profits of up to 3 staff and non-profits; "not permissible to duplicate or alter ... with the intention to distribute"; larger companies need a licence ([license.md](https://huggingface.co/datasets/ai4privacy/pii-masking-200k/raw/main/license.md)) | **No**; download only |
| [ai4privacy/pii-masking-300k](https://huggingface.co/datasets/ai4privacy/pii-masking-300k), [400k](https://huggingface.co/datasets/ai4privacy/pii-masking-400k) | Earlier flagship sets | `license: other` (`license.md`); "Academic use is encouraged ... Commercial entities should contact us" | **No** |
| [ai4privacy/open-pii-masking-500k-ai4privacy](https://huggingface.co/datasets/ai4privacy/open-pii-masking-500k-ai4privacy) | 20 labels, built with Llama 3.1/3.3 | Subject to the Llama Community License and Acceptable Use Policy; include the licence text when redistributing | Only with Llama licence obligations |
| [presidio-research](https://github.com/data-privacy-stack/presidio-research/tree/7f568b6da8a5) | Template-based fake-data generator plus `data/synth_dataset_v2.json` (about 1 MB) | Repo MIT; its generator docs carry a Fake Name Generator CC BY-SA 3.0 copyright notice ([data_generation.md](https://github.com/data-privacy-stack/presidio-research/blob/7f568b6da8a5/docs/data_generation.md)) | Check whether the share-alike notice reaches the committed JSON (UNVERIFIED); safer to generate |
| CoNLL-2003 English | Four types only (PER, ORG, LOC, MISC); Reuters news Aug 1996-Aug 1997; exact-match entity F1 ([paper](https://aclanthology.org/W03-0419.pdf)) | The shared-task paper says the data files "except the words" are distributed from the task site, i.e. the raw Reuters text comes separately | No; and it has no email/phone/SSN/card labels, so it cannot test the regex tier |
| OntoNotes 5.0 | 18 NER types incl. PERSON, GPE, DATE; trained into spaCy models | LDC licence, fee for non-members ([LDC2013T19](https://catalog.ldc.upenn.edu/LDC2013T19)) | No; not PII-specific |

Notes [analysis]: ai4privacy label sets use `GIVENNAME`/`SURNAME` where Presidio uses `PERSON` and `SOCIALNUM` mixes national formats, so write an explicit entity mapping (presidio-research documents the need: [entity_hierarchy.md](https://github.com/data-privacy-stack/presidio-research/blob/7f568b6da8a5/docs/entity_hierarchy.md)). The open ai4privacy 19-label set has no IBAN label, so generate IBAN and Luhn-valid card cases yourself with known check digits. Split by template to avoid leakage (presidio-research notebook 3 exists for this), and keep a development set separate from a held-out set, as pii-shield does ([README](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/README.md)).

### 7.2 Computing per-entity precision and recall

- CoNLL-style entity-level scoring: "A named entity is correct only if it is an exact match of the corresponding entity in the data file" ([CoNLL-2003 paper](https://aclanthology.org/W03-0419.pdf)). seqeval's default mode simulates `conlleval`; strict mode applies a chosen tagging scheme, and its docs show a case scoring 1.00 in default and 0.00 in strict ([seqeval](https://github.com/chakki-works/seqeval)).
- nervaluate reports four schemas: strict (exact boundary and type), exact (boundary, any type), partial (partial boundary), type (some overlap) ([nervaluate](https://github.com/MantisAI/nervaluate)).
- presidio-evaluator matches spans by IoU with a threshold (docs use 0.75 as an example), character- or token-based, at binary, branch and detailed entity levels ([span_evaluation.md](https://github.com/data-privacy-stack/presidio-research/blob/7f568b6da8a5/docs/span_evaluation.md), [span_matching_strategies.md](https://github.com/data-privacy-stack/presidio-research/blob/7f568b6da8a5/docs/span_matching_strategies.md)). Presidio's evaluation page recommends F-beta with beta = 2 because missing PII usually costs more than a false alarm ([evaluation index](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/evaluation/index.md)).
- Recommended report for a masking proxy [analysis]: (1) strict span P/R per entity type; (2) overlap/IoU >= 0.5 P/R per type; (3) **character-level leak rate** = share of gold PII characters not covered by any mask (a half-masked SSN still leaks digits); (4) type-confusion matrix; (5) false-positive rate on PII-free documents; (6) F2 as the headline. Because masking is reversible, an over-wide span is restored and mostly costs task quality, not privacy.

### 7.3 Latency method

- Upstream must be a **local mock** that emits a fixed SSE script with configurable inter-chunk delay, so numbers isolate proxy overhead; the real API's variance would swamp it [analysis].
- Measure: (1) non-stream added latency = proxy minus direct at the same offered rate; (2) stream time-to-first-data-event delta, per-event added delay, and total time; (3) CPU and RSS of proxy and Redis; (4) errors and truncated streams. Sweep prompt size (100 B to 100 KB), PII density (0, 1, 10, 100 entities), concurrency, chunk cadence, regex-only vs regex+Presidio, Lua vs non-Lua allocation.
- **Coordinated omission**: closed-loop tools wait for each response before sending the next, hiding queueing during stalls. the wrk2 README says plain wrk's latency model "exhibits a strong Coordinated Omission effect"; wrk2 takes a constant rate (`-R`) and "measures response latency from the time the transmission should have occurred", using HdrHistogram ([wrk2 README](https://github.com/giltene/wrk2)); k6 documents the closed model and calls the effect coordinated omission, and its `constant-arrival-rate` executor is the open model ([k6 docs](https://grafana.com/docs/k6/latest/using-k6/scenarios/concepts/open-vs-closed/)). Gil Tene's talk "How NOT to Measure Latency" is the standard reference ([video](https://www.youtube.com/watch?v=lJ8ydIuPFeU)).
- Tool fit for SSE:

| Tool | Open-loop rate | SSE per-event timing | Status |
|---|---|---|---|
| [wrk2](https://github.com/giltene/wrk2) | Yes (`-R`) | No (Lua hooks, no stream support stated) | README says "experimental/development mode"; last push 2024-03-03 |
| [vegeta](https://github.com/tsenart/vegeta) | Yes (`-rate`); README says it "Avoids" coordinated omission | Not documented; usable as a Go library | Pushed 2026-09-24 |
| [oha](https://github.com/hatoo/oha) | `-q` plus `--latency-correction` | Not documented | Pushed 2026-10-19 |
| k6 | `constant-arrival-rate` | Via third-party [xk6-sse](https://github.com/phymbert/xk6-sse) extension; native support UNVERIFIED | Active |

  Since none documents time-to-first-event for SSE, write a small purpose-built load client (open-loop schedule, intended-start timestamps, per-event arrival times, HdrHistogram) and use wrk2 or vegeta only for non-streaming cross-checks [analysis]. Report p50/p90/p99/p99.9/max with sample counts, warm-up excluded; for p99 collect at least about 10,000 samples per cell [analysis]; run on Linux, pinned CPUs, client and mock on different cores (Docker Desktop on macOS adds a VM layer) [analysis].

---

## 8. Threat model gaps and the "what it misses" table

Relevant OWASP Top 10 for LLM Applications (2025) entries ([list](https://genai.owasp.org/llm-top-10/)): LLM01 Prompt Injection (includes an indirect-injection scenario where hidden instructions make the model insert an image linking to an attacker URL so the private conversation is sent to the attacker, [LLM01](https://genai.owasp.org/llmrisk/llm01-prompt-injection/)); LLM02 Sensitive Information Disclosure (lists PII leakage; prevention mentions redacting confidential content with pattern matching and tokenization, [LLM02](https://genai.owasp.org/llmrisk/llm022025-sensitive-information-disclosure/)); LLM05 Improper Output Handling and LLM07 System Prompt Leakage are adjacent.

Draft rows for the README:

| # | Miss | Mechanism | How to measure | Source |
|---|---|---|---|---|
| 1 | Names, addresses, free-form identifiers not matched by regex | Regex tier has no NER; Presidio+spaCy is context-dependent | Per-entity recall on Nemotron-PII / openpii-mini; regex-only vs +Presidio | Presidio notebook 4: binary recall 0.646 ([nb4](https://github.com/data-privacy-stack/presidio-research/blob/7f568b6da8a5/notebooks/4_Evaluate_Presidio_Analyzer.ipynb)) |
| 2 | False positives on look-alikes (order ID as card, employee ID as phone) | Checksum passes 1 in 10; no checksum for phones | FP rate on PII-free corpus | [Luhn check](#31-luhn-and-card-numbers) [my check]; pii-shield FP table ([README](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/README.md)) |
| 3 | Model transforms the token (lowercase, translate, paraphrase, drop) | Restoration is exact-string match | Round-trip rate with a scripted mock "model" that mangles tokens at configurable rates | LLM Guard fuzzy modes; PrivAiTe threat model ([README](https://github.com/crp4222/PrivAiTe/blob/40ee53f559de/README.md)) |
| 4 | Truncated token at `max_tokens` | Stream ends mid-token | Count of streams ending in a partial token | [LiteLLM heuristic](https://github.com/BerriAI/litellm/blob/2d181e663373/litellm/proxy/guardrails/guardrail_hooks/presidio.py#L1304-L1327) |
| 5 | Quasi-identifiers survive masking | Combinations of non-masked fields identify people: "87% (216 million of 248 million) of the population in the United States ... unique based only on {5-digit ZIP, gender, date of birth}"; 53% by {place, gender, DOB}; 18% by {county, gender, DOB} (1990 Census) | Not measurable by this tool; state it | [Sweeney 2000](https://dataprivacylab.org/projects/identifiability/paper1.pdf) |
| 6 | PII in non-message request fields | Spec lists `tools` (function schemas), `response_format`, `prediction`, `metadata`, `user`, `safety_identifier`, `prompt_cache_key`, `logit_bias`, `stop`, and non-text message content parts (the `messages` description mentions images and audio) | Field-coverage matrix: plant a canary value in each field, count leaks upstream | [openapi.yaml](https://github.com/openai/openai-openapi/blob/main/openapi.yaml); Bedrock does not evaluate tool args, tool results or tool definitions ([docs](https://docs.aws.amazon.com/bedrock/latest/userguide/guardrails-sensitive-filters.html)); LangSmith skips system/developer prompts and single-string tool arguments ([docs](https://docs.langchain.com/langsmith/llm-gateway-redaction)) |
| 7 | Assistant-role history re-sent by the client contains real values | Client received rehydrated output and sends it back on the next turn | Multi-turn test: same value must map to the same token across turns | [analysis] |
| 8 | PII the model generates that was not in the input (hallucinated or memorised) | Rehydration only maps known tokens; output is not scanned | Output-side detection rate (out of MVP) | Training-data extraction recovers names, phone numbers, emails ([Carlini et al.](https://arxiv.org/abs/2012.07805)); LiteLLM offers `presidio_filter_scope` output/both ([docs](https://docs.litellm.ai/docs/proxy/guardrails/pii_masking_v2)) |
| 9 | **Rehydration as an exfiltration channel** | Injected instruction tells the model to put every token it has seen into a URL or markdown image; the proxy restores the real values into the URL before the client renders it | Test: injected prompt + token-in-URL output; count restored values inside URL/markdown/HTML contexts | OWASP LLM01 image-URL scenario ([LLM01](https://genai.owasp.org/llmrisk/llm01-prompt-injection/)); indirect injection ([Greshake et al.](https://arxiv.org/abs/2302.12173)); [analysis] for the interaction with rehydration; mitigation: do not rehydrate inside URL/markdown-link/HTML contexts unless allowed |
| 10 | Token guessing and cross-session rehydration | Shared vault with unscoped lookups; short guessable suffix | Cross-session test: token from session A in session B's response must stay opaque | pii-shield's per-request-vault rationale ([README](https://github.com/Anwesha33/pii-shield/blob/43572f6e7434/README.md)); 16-bit suffix collision numbers in 5.4 [my check] |
| 11 | Vault compromise | Redis default user has no password and all commands; values readable if unencrypted | Config review; test that Redis holds only ciphertext | [ACL docs](https://redis.io/docs/latest/operate/oss_and_stack/management/security/acl/); closemask stores plaintext ([redis_manager.go](https://github.com/huilangsh/closemask/blob/2c771e14bfc2/internal/session/redis_manager.go)) |
| 12 | Log leakage | Debug logs of bodies, panics echoing input, Redis `MONITOR` | Grep test: run canaries, scan all logs and Redis for plaintext | Bedrock logs keep originals and guardrail trace `match` returns the original ([docs](https://docs.aws.amazon.com/bedrock/latest/userguide/guardrails-sensitive-filters.html)) |
| 13 | Size and timing side channels | Token substitution changes lengths; streamed token lengths leak content | State it; optionally keep upstream `obfuscation` consistent | OpenAI's `obfuscation` field exists to mitigate "certain side-channel attacks" ([openapi.yaml](https://github.com/openai/openai-openapi/blob/main/openapi.yaml)); token-length attack reconstructed 27% of responses and inferred topic of 53% ([Weiss et al., USENIX Security 2024](https://www.usenix.org/conference/usenixsecurity24/presentation/weiss)) |
| 14 | The proxy is a fully trusted party | Terminates TLS, sees all plaintext and the upstream API key | State it | [analysis] |
| 15 | Rehydrated values are attacker-influenced strings | A value matched as "email" can contain markup-like characters | Output-encoding test in the demo app | OWASP LLM05 Improper Output Handling ([list](https://genai.owasp.org/llm-top-10/)) |
| 16 | Presidio sidecar has no auth | By design | Compose network isolation | [faq.md](https://github.com/data-privacy-stack/presidio/blob/4f0e236e31a5/docs/faq.md) |

Legal framing [analysis]: say "pseudonymization", not "anonymization"; make no compliance claims.

---

## 9. Language decision

### 9.1 Facts by language

| Need | Go | Java | Python |
|---|---|---|---|
| Streaming proxy primitive | `httputil.ReverseProxy`: `Rewrite`, `ModifyResponse` (can replace the body), `ErrorHandler`, `BufferPool`; flushes immediately for `text/event-stream` or unknown content length ([docs](https://pkg.go.dev/net/http/httputil), [source](https://github.com/golang/go/blob/master/src/net/http/httputil/reverseproxy.go)) | No stdlib reverse proxy. JDK `HttpClient` body handlers `ofInputStream`, `ofLines`, `ofPublisher` deliver the body as it arrives ([javadoc](https://docs.oracle.com/en/java/javase/21/docs/api/java.net.http/java/net/http/HttpResponse.BodyHandlers.html)); virtual threads (JDK 21) allow thread-per-request code ([JEP 444](https://openjdk.org/jeps/444)); Vert.x HTTP proxy has body-transform interceptors but "Most transformations ... are synchronous and buffer bytes" with a 256K default limit ([docs](https://vertx.io/docs/vertx-http-proxy/java/)); Spring Cloud Gateway `ModifyResponseBody` doc does not say how the body is buffered ([docs](https://docs.spring.io/spring-cloud-gateway/reference/spring-cloud-gateway-server-webflux/gatewayfilter-factories/modifyresponsebody-factory.html)); Spring `WebClient` shows `bodyToFlux` with `TEXT_EVENT_STREAM` ([docs](https://docs.spring.io/spring-framework/reference/web/webflux-webclient/client-retrieve.html)) | httpx `AsyncClient.stream` with `aiter_bytes`/`aiter_lines` ([docs](https://www.python-httpx.org/async/)); FastAPI `StreamingResponse` takes an async generator, and the docs point to "Stream Data" patterns for cancellation handling ([docs](https://fastapi.tiangolo.com/advanced/custom-response/)); uvloop claims 2-4x over asyncio ([README](https://github.com/MagicStack/uvloop)) |
| Regex | RE2: linear time, no lookaround (section 3.6) | Full-featured, backtracking; RE2/J available | Full-featured; Presidio uses `regex` with timeouts |
| Concurrency / parallelism | Goroutines | Virtual threads (JDK 21) | GIL: "only one thread executes Python bytecode at a time", released during I/O; optional free-threaded build since 3.13 ([glossary](https://docs.python.org/3/glossary.html#term-global-interpreter-lock)) |
| Redis client | go-redis (official docs show it; v9.23.0, 2026-10-05) with `Script` helper that retries `NOSCRIPT` ([script.go](https://github.com/redis/go-redis/blob/master/script.go)) | Jedis (v8.0.2, 2026-10-07) and Lettuce (7.9.0, 2026-10-14), both shown in Redis' command docs ([SET](https://redis.io/docs/latest/commands/set/)) | redis-py |
| Phone validation | nyaruka/phonenumbers (third-party port) | libphonenumber (reference) | python-phonenumbers (used by Presidio) |
| Presidio integration | REST sidecar | REST sidecar | In-process library or sidecar |
| Fuzzing / property tests | Native fuzzing in the toolchain since 1.18; one target per test, 1-second execution limit ([docs](https://go.dev/doc/security/fuzz/)) | Third-party libraries (not verified) | Third-party (hypothesis; not verified) |
| Load tooling in the same language | vegeta is a Go library and CLI ([README](https://github.com/tsenart/vegeta)) | n/a | n/a |

### 9.2 Recommendation

Goal as stated (credible infra repo with p99 numbers, 2-3 weeks, owner stronger in Java, leaning to finish fast) [judgement built on the table above]:

- **Java 21** with JDK `HttpClient` (stream bodies) plus virtual threads, or Vert.x with a custom streaming handler (not its buffering transformers), Lettuce or Jedis, and RE2/J for the detection regexes if you want the linear-time guarantee. You write the proxy plumbing yourself (hop-by-hop headers, cancellation, backpressure), which Go's `ReverseProxy` gives you, but you write it in a language you know. Warm up the JVM before measuring so the p99 reflects steady state (standard practice; I did not find a primary source to cite, so treat as [analysis]).
- The parts that decide the repo's quality are language-neutral: the rewriter state machine, the JSON walk, the benchmark harness and the miss table. Implement the rewriter and detectors as pure, I/O-free units with a test corpus so a port is mechanical.
- **Python** is the fastest to a demo and easiest for Presidio, but a Python proxy under load measures Python and Presidio, not "a small proxy"; the GIL limits parallelism for CPU-bound rewriting. Use it only if you drop the p99 claim.
- **If the goal is a Go-shop signal:** choose Go. What changes: budget a few extra days up front (your estimate, not mine) for Go idioms; `ReverseProxy` removes proxy plumbing; RE2 forces candidate-then-validate regex design (which you need anyway); native fuzzing fits the rewriter tests; vegeta can be embedded in the harness; beware `encoding/json` defaults (HTML escaping of `<`, `>`, `&`; float64 for numbers when decoding into `any`; key re-ordering) when rewriting bodies: decode with `UseNumber` or edit raw JSON tokens instead of round-tripping through `map[string]any` ([encoding/json](https://pkg.go.dev/encoding/json)). A time-box such as "passthrough + SSE working by day 3, else switch" is cheap insurance [analysis].

---

## 10. Differentiation verdict, scope traps, cut line, plan critique

### 10.1 Candidate angles scored against the evidence

| Angle | Evidence | Verdict |
|---|---|---|
| Boundary-safe streaming with a tested state machine and property/fuzz tests | Cosy tests every split position of a 75-byte placeholder; pii-shield tests every split of one token; maskit has split tests; PasteGuard has per-(choice, tool-call) channels | Table stakes, not a headline. Still do it, but extend to what the others skip: `n>1` interleave, tool-call arguments with inner JSON escaping, finish-reason ordering, usage chunk, mid-stream error, random chunking and near-miss prefixes (fuzz), published as a conformance suite |
| Reproducible benchmark harness with mock upstream (open-loop p50/p99, TTFT delta) | No repo or doc I inspected publishes proxy-level overhead; LLM Guard publishes scanner latency, pii-shield detector latency | **Best angle** (absence of evidence; search was not exhaustive) |
| Honest miss table with measured miss rates | pii-shield and PrivAiTe list limitations; PrivAiTe measures tool-call leaks for LiteLLM and LLM Guard (vendor-reported) | Good; make it measured per field (canary planted in each request field) and reproducible |
| Deterministic per-session tokens | token-proxy (HMAC with per-session salt), pii-shield (same value, same token), closemask (Redis session) | Not unique |
| Fail-closed on unparseable bodies and vault errors | PrivAiTe fails closed; LiteLLM fails open by default (user-reported [#30728](https://github.com/BerriAI/litellm/issues/30728)) | Cheap, worth stating, modest differentiation |
| Tool-call / JSON-aware masking | PasteGuard, PrivAiTe, Cosy do it; Bedrock does not; LangSmith partially | Table stakes for agent traffic in 2026; implement in the MVP |
| Context-aware rehydration (do not restore into URLs/markdown/HTML) | Not seen in any tool I read (not exhaustive); grounded in OWASP LLM01 scenario | Unproven hypothesis but a real security angle with a testable claim; small to implement as a policy |
| Redis session vault, encrypted, atomic, with a Lua vs no-Lua measurement | closemask has Redis with plaintext HSET; ocultar has an encrypted DuckDB vault | Modest; worth doing because it feeds the benchmark |

Net: the repo stands out as "the proxy with receipts": numbers, a harness anyone can rerun, a field-coverage matrix and a conformance suite. It does not stand out as a feature set.

### 10.2 Scope traps

- Presidio sidecar tuning and NER quality work (default one worker, 382 MiB model, quality is its own project).
- Format-preserving tokens (FF1 key management, domain limits, FF3 gone) and Faker fakes (collision handling).
- Policy file with mask/hash/block per entity: hash needs a keyed design (section 5.4); keep mask and block only.
- Adapters for Anthropic Messages and the Responses API (each is a new SSE dialect and new bug class, as LiteLLM's issues show).
- GIF, architecture polish, dashboards (PasteGuard already has a UI).
- Multi-tenant auth, Redis Cluster, Functions, Kubernetes.
- Multilingual detection, image/PDF/OCR masking.
- Chasing a "compliance" story.

### 10.3 MVP definition (the cut line)

Ship when all of these hold:

1. OpenAI chat-completions proxy, stream and non-stream, passthrough verified byte-for-byte when nothing is detected.
2. Detectors: email, phone (with region hint), SSN-shaped (dashes or context), card (Luhn + issuer + length), IBAN (mod-97). Per-entity action: mask or block.
3. JSON-aware walk: message text and content parts, assistant `tool_calls` arguments, optional tool-schema scanning; unparseable or oversized bodies rejected (fail closed).
4. Vault: Redis, authenticated-session-scoped, random 8+ hex tokens, AES-GCM values, TTL, no persistence; one allocation strategy plus one alternative for the benchmark.
5. Streaming rewriter per (choice, channel), viable-prefix hold-back, flush rules, conformance and fuzz tests.
6. Harness: mock upstream, open-loop client with TTFT, results as JSON plus a table.
7. Benchmark: per-entity strict and overlap P/R plus leak rate on one CC-BY dataset (Nemotron-PII test split) and generated IBAN/card cases; held-out split; PII-free FP corpus.
8. README: value statement limited to what is measured, threat model, miss table, benchmark table labelled "synthetic set".

Optional if time remains: Presidio sidecar as one extra benchmark row; Anthropic adapter.

### 10.4 Week-by-week critique of the plan

| Plan | Problem | Suggested |
|---|---|---|
| W1: proxy, regex rules, Redis vault with Lua, streaming passthrough | Four things in one week; Lua is optional (6.1); measurement is deferred to W3, so overhead problems surface late | W1: passthrough proxy + mock upstream + harness skeleton (record the baseline first), regex tier, in-memory vault. Get a first p99 number by end of week 1 |
| W2: Presidio sidecar, format-preserving tokens, policy file | Three separate projects; FPE needs FF1 only and a key story; policy `hash` is unsafe unkeyed | W2: streaming rewriter + conformance/fuzz tests, JSON-aware walk, Redis vault (encrypted, session-scoped, Lua vs non-Lua), fail-closed. Presidio only as an adapter behind an interface |
| W3: benchmark (P/R, regex vs regex+Presidio, p50/p99) + README | Benchmark design and dataset licensing eat more than a week if started in W3 | W3: dataset + P/R, load sweeps, optional Presidio row, miss table with measured rows, README; stop adding features by mid-week |

### 10.5 Claims in the plan that look wrong or overstated

- "Lua scripts for atomic token allocation": not required for random tokens (section 6.1).
- "Official Presidio Docker images": published on ghcr.io under Data Privacy Stack; MCR images are stale (section 4).
- "Streaming re-hydration is the hardest part": the single-split case is solved in several repos; the harder parts are breadth (channels, dialects), benchmark credibility and scope control.
- "Format-preserving tokens": avoid FF3/FF3-1 given NIST's 2025 draft (section 5.1); evidence on LLM quality by token style is thin (section 5.3).
- "Any LLM API": three SSE dialects (section 2.6).
- "Benchmark datasets": ai4privacy 200k/300k/400k cannot be vendored; use CC-BY sets (section 7.1).
- "Hash policy": unkeyed hashes of SSNs/phones are enumerable (section 5.4).
- "Distinct from LiteLLM+Presidio or LLM Guard": true for streaming behaviour and vault, but the closer competitors are the small proxies in section 1.2, one of which has the same stack and plan.
- "Don't claim production-grade": correct; keep it.

---

## Sources checked

Primary sources opened on 2026-10-19 (repos as shallow clones at the commit shown; docs via fetch):

Presidio and related
- Presidio repo @4f0e236 (analyzer `app.py`, Dockerfile, entrypoint, conf, recognizers, anonymizer operators, docs incl. `installation.md`, `faq.md`, `evaluation/`, `supported_entities.md`, `project_transition.md`, samples): https://github.com/data-privacy-stack/presidio
- Presidio docs (anonymizer page): https://presidio.dataprivacystack.org/anonymizer/
- presidio-research @7f568b6 (README, docs, notebooks 4 and 5, `data/`): https://github.com/data-privacy-stack/presidio-research
- spaCy model releases: https://github.com/explosion/spacy-models/releases (en_core_web_sm/md/lg/trf 3.8.0)

Gateways and guardrails
- LiteLLM @2d181e6 `presidio.py`: https://github.com/BerriAI/litellm ; docs https://docs.litellm.ai/docs/proxy/guardrails/pii_masking_v2 ; issues 46087, 42476, 41611, 30728
- LLM Guard @168c103: https://github.com/protectai/llm-guard
- NeMo Guardrails @3a6e84f: https://github.com/NVIDIA-NeMo/Guardrails
- Helicone: https://docs.helicone.ai/ , https://github.com/Helicone/helicone
- Portkey: https://portkey.ai/docs/product/guardrails/pii-redaction
- Lakera: https://docs.lakera.ai/docs/api/guard
- Protecto: https://docs.protecto.ai/
- Cloudflare: https://developers.cloudflare.com/ai-gateway/features/dlp/ , https://developers.cloudflare.com/ai-gateway/features/guardrails/
- Kong: https://developer.konghq.com/plugins/ai-sanitizer/
- Guardrails AI: https://guardrailsai.com/hub/validator/guardrails/detect_pii
- Bedrock: https://docs.aws.amazon.com/bedrock/latest/userguide/guardrails-sensitive-filters.html
- LangSmith gateway: https://docs.langchain.com/langsmith/llm-gateway-redaction
- OSS proxies: pasteguard @95df2a7, pii-shield @43572f6, closemask @2c771e1, CosyRedactGateway @07e0c60, maskit @b217a9b, token-proxy @8e64148, PrivAiTe @40ee53f, ocultar @b48d967, pii-proxy @183b057, privacy-filter @64b8de3, DontFeedTheAI @2c7e284, llm-dlp @c011c7c (URLs in section 1.2)

Protocol and platform docs
- OpenAI OpenAPI spec: https://github.com/openai/openai-openapi/blob/main/openapi.yaml ; Python SDK: https://github.com/openai/openai-python/blob/main/src/openai/_streaming.py
- Anthropic streaming: https://platform.claude.com/docs/en/build-with-claude/streaming
- WHATWG SSE parsing: https://html.spec.whatwg.org/multipage/server-sent-events.html#parsing-an-event-stream
- Go: https://pkg.go.dev/net/http/httputil , https://github.com/golang/go/blob/master/src/net/http/httputil/reverseproxy.go , https://pkg.go.dev/regexp , https://github.com/google/re2/wiki/Syntax , https://pkg.go.dev/encoding/json , https://pkg.go.dev/net/mail , https://go.dev/doc/security/fuzz/ , https://github.com/golang/go/issues/11646 , https://github.com/golang/go/issues/26623
- Java: https://docs.oracle.com/en/java/javase/21/docs/api/java.base/java/util/regex/Pattern.html , https://docs.oracle.com/en/java/javase/21/docs/api/java.net.http/java/net/http/HttpResponse.BodyHandlers.html , https://openjdk.org/jeps/444 , https://github.com/google/re2j , https://vertx.io/docs/vertx-http-proxy/java/ , https://docs.spring.io/spring-cloud-gateway/reference/spring-cloud-gateway-server-webflux/gatewayfilter-factories/modifyresponsebody-factory.html , https://docs.spring.io/spring-framework/reference/web/webflux-webclient/client-retrieve.html
- Python: https://www.python-httpx.org/async/ , https://fastapi.tiangolo.com/advanced/custom-response/ , https://github.com/MagicStack/uvloop , https://docs.python.org/3/glossary.html#term-global-interpreter-lock
- Redis: https://redis.io/docs/latest/commands/set/ , https://redis.io/docs/latest/commands/hsetnx/ , https://redis.io/docs/latest/commands/hexpire/ , https://redis.io/docs/latest/develop/using-commands/transactions/ , https://redis.io/docs/latest/develop/programmability/eval-intro/ , https://redis.io/docs/latest/develop/programmability/functions-intro/ , https://redis.io/docs/latest/operate/oss_and_stack/management/security/encryption/ , https://redis.io/docs/latest/operate/oss_and_stack/management/security/acl/ ; go-redis https://github.com/redis/go-redis

Standards, papers, datasets
- SSA POMS RM 10201.035: https://secure.ssa.gov/poms.nsf/lnx/0110201035
- Luhn patent US 2,950,048: https://patents.google.com/patent/US2950048A/en
- RFC 5322: https://www.rfc-editor.org/rfc/rfc5322 ; RFC 2104: https://www.rfc-editor.org/rfc/rfc2104 ; WHATWG email: https://html.spec.whatwg.org/multipage/input.html#valid-e-mail-address
- libphonenumber: https://github.com/google/libphonenumber ; Go port: https://github.com/nyaruka/phonenumbers
- NIST SP 800-38G: https://csrc.nist.gov/pubs/sp/800/38/g/final , https://csrc.nist.gov/pubs/sp/800/38/g/r1/ipd , https://csrc.nist.gov/pubs/sp/800/38/g/r1/2pd
- Faker: https://faker.readthedocs.io/en/master/
- OWASP LLM Top 10 (2025): https://genai.owasp.org/llm-top-10/ , https://genai.owasp.org/llmrisk/llm01-prompt-injection/ , https://genai.owasp.org/llmrisk/llm022025-sensitive-information-disclosure/
- Sweeney 2000: https://dataprivacylab.org/projects/identifiability/paper1.pdf
- Carlini et al.: https://arxiv.org/abs/2012.07805 ; Greshake et al.: https://arxiv.org/abs/2302.12173 ; Weiss et al.: https://www.usenix.org/conference/usenixsecurity24/presentation/weiss
- Deußer et al.: https://arxiv.org/abs/2609.11335 ; SurrogateShield: https://arxiv.org/abs/2606.29567
- Dataset cards: https://huggingface.co/datasets/nvidia/Nemotron-PII , https://huggingface.co/datasets/gretelai/gretel-pii-masking-en-v1 , https://huggingface.co/datasets/ai4privacy/pii-masking-200k (+ `license.md`), .../pii-masking-300k, .../pii-masking-400k, .../open-pii-masking-500k-ai4privacy, .../pii-masking-openpii-1.5m, .../openpii-masking-mini-10k
- CoNLL-2003 paper: https://aclanthology.org/W03-0419.pdf ; OntoNotes 5.0: https://catalog.ldc.upenn.edu/LDC2013T19
- seqeval: https://github.com/chakki-works/seqeval ; nervaluate: https://github.com/MantisAI/nervaluate
- Load tools: https://github.com/giltene/wrk2 , https://github.com/tsenart/vegeta , https://github.com/hatoo/oha , https://grafana.com/docs/k6/latest/using-k6/scenarios/concepts/open-vs-closed/ , https://github.com/phymbert/xk6-sse , https://www.youtube.com/watch?v=lJ8ydIuPFeU

My own checks (scratch only): Luhn pass-rate simulation (200,000 random digit strings per length 13-19, seed 1); Presidio card regex tested on 10 sample formats; SSN structure arithmetic; birthday-bound table for 16-bit tags.

---

## Unverified / couldn't confirm

1. **ISO/IEC 7812-1 (PAN length, IIN ranges, 8-digit IIN change) and ISO 13616 (IBAN):** iso.org and swift.com returned 403 to my fetcher. Card length/issuer rules and IBAN specifics rest on Presidio's implementation and on pii-shield's README, not on the standards. Mastercard 2-series ranges are also unsourced.
2. **SSA randomization pages** (ssa.gov/employer/randomization*.html) returned 403. Invalid-SSN rules are confirmed from POMS RM 10201.035 only; the June 2011 randomization date is from search summaries.
3. **Whether a final NIST SP 800-38G Rev. 1 exists.** I found the 2025-02-03 second public draft and no final page (searched CSRC and NIST only). The FF3-1 status after the 2PD is inferred from "The encryption method FF3 is no longer specified" plus the Beyne statement.
4. **Presidio latency, throughput and memory numbers:** none in the docs or repo; GHCR image size not checked. Any sidecar figure must be measured.
5. **Redis OSS encryption-at-rest and persistence guidance:** not fetched; the "disable RDB/AOF" advice is reasoning, not a cited recommendation. `SET ... NX GET` version support was not stated on the page I read (only that `GET` is since 6.2.0).
6. **Competitor claims not reproduced:** PrivAiTe's recall table, pii-proxy's F1 numbers, pii-shield's P/R and microsecond latency, maskit's and token-proxy's streaming claims beyond the tests I opened, Cosy's calibration figure. I did not run any of these projects.
7. **Placeholder style vs LLM quality:** the two 2026 preprints were checked at abstract level only; neither isolates opaque unique tokens against fakes. I did not read the Redakto result that appeared in search.
8. **k6 native SSE support:** only the third-party `xk6-sse` extension was verified. Whether any listed load tool reports time-to-first-SSE-event natively is unconfirmed (docs silent).
9. **Absence claims:** "no one publishes proxy-level p50/p99 overhead" and "no tool does context-aware rehydration" come from reading about a dozen READMEs/docs and grepping source; the GitHub search was not exhaustive and results are dominated by repos created in 2026.
10. **Spring Cloud Gateway `ModifyResponseBody` buffering behaviour** and **Vert.x behaviour for unmodified streaming bodies:** docs are silent.
11. **Go regexp vs Java/Python speed:** only a community benchmark pasted in Go issue #26623; no official numbers. Whether regex time matters at prompt sizes in the 1-100 KB range is unmeasured.
12. **Fetch-tool caveat:** the Portkey, Lakera, Protecto, Cloudflare, Kong, Guardrails AI, Helicone, LangSmith and Bedrock statements came through a summarising fetch tool; wording may be paraphrased.
13. **presidio-research `synth_dataset_v2.json` licence:** the repo is MIT but the generator docs carry a CC BY-SA 3.0 notice; I did not determine whether it applies to the committed file.
14. **OpenAI-compatible third-party servers' SSE deviations** (vLLM, Ollama, etc.) were not checked.
