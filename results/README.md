# Results

Each directory is one run of a script, kept so the numbers in the README can be
checked and re-derived.

| Directory | Script | What it holds |
|---|---|---|
| `laptop-3/` | `scripts/bench.sh` | Overhead matrix: JSON per run, `REPORT.md` with every column. Apple M4 laptop, no Docker, generator, proxy and mock on one machine. Not a quiet-host benchmark. |
| `redis-2/` | `REDIS_URL=... scripts/bench.sh` | (Superseded by `redis-3/`, kept for the record.) Overhead with Redis as the vault: Lua allocation and `HSETNX` rows next to the in-memory vault and the baseline. Redis 7.4.11 in Docker Desktop on the same laptop. |
| `redis-3/`, `redis-3-long/` | `REDIS_URL=... RUN=redis-3 PAYLOADS="pii pii-long" DURATION=25s scripts/bench.sh` (the `-long` directory is a second pass of `PAYLOADS=pii-long` because the first pass had flagged baseline cells; its report against the valid repeat baseline is `REPORT-baseline-repeat.md`) | As `redis-2/` plus a row for Lua allocation with concurrent single calls (`-redis-batch=false`), next to the pipelined default. |
| `rivals-lowrate/` | `RUN=lowrate MODE=lowrate scripts/rivals.sh` (run as a loose script first; same loop) | Streaming TTFT at 2 req/s, 60 s: direct, this proxy in a container, LiteLLM 1.106.0 + Presidio. Separates LiteLLM's buffering from analyzer saturation. |
| `rivals-overhead/` | `scripts/rivals.sh` | The same at 10 req/s (JSON and SSE), 40 s per cell; `images.txt` pins the image digests. |
| `rivals-nemotron/`, `rivals-synthetic/` | `RUN=... DATA=... MODE=eval scripts/rivals.sh` | Detection and the 21-field matrix for this proxy (container) and LiteLLM + Presidio, on Nemotron-PII and on the synthetic set. |
| `presidio/` | `eval presidio` | The Presidio analyzer called directly, at score thresholds 0 and 0.5. |
| `eval-synthetic/` | `scripts/eval.sh` | Detector-only and through-the-proxy scores on 2,000 seeded synthetic samples; request-field matrix. |
| `eval-nemotron/` | `DATA=data/nemotron-pii.jsonl scripts/eval.sh` | The same on the first 3,000 test rows of `nvidia/Nemotron-PII` (CC-BY-4.0; the data itself is not stored here). |

Earlier benchmark runs were discarded because the mock upstream's reply length
varied with the request (see the commit history); only runs made after that fix
are kept. Logs are not committed.
