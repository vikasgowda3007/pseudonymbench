# Results

Each directory is one run of a script, kept so the numbers in the README can be
checked and re-derived.

| Directory | Script | What it holds |
|---|---|---|
| `laptop-3/` | `scripts/bench.sh` | Overhead matrix: JSON per run, `REPORT.md` with every column. Apple M4 laptop, no Docker, generator, proxy and mock on one machine. Not a quiet-host benchmark. |
| `eval-synthetic/` | `scripts/eval.sh` | Detector-only and through-the-proxy scores on 2,000 seeded synthetic samples; request-field matrix. |
| `eval-nemotron/` | `DATA=data/nemotron-pii.jsonl scripts/eval.sh` | The same on the first 3,000 test rows of `nvidia/Nemotron-PII` (CC-BY-4.0; the data itself is not stored here). |

Earlier benchmark runs were discarded because the mock upstream's reply length
varied with the request (see the commit history); only runs made after that fix
are kept. Logs are not committed.
