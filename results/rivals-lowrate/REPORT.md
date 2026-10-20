| setup | payload | mode | rps | total p50 (ms) | total p99 (ms) | Δp50 | Δp99 | TTFT p50 (ms) | TTFT p99 (ms) | ΔTTFT p50 | ΔTTFT p99 | errors | valid |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| direct | pii | sse | 2 | 3243.7 | 3428.2 | baseline | baseline | 152.6 | 214.9 | baseline | baseline | 0/120 | true |
| direct-repeat | pii | sse | 2 | 3244.5 | 3396.6 | +0.8 | -31.6 | 152.5 | 178.4 | -0.1 | -36.5 | 0/120 | true |
| litellm-presidio | pii | sse | 2 | 3318.1 | 3651.4 | +74.5 | +223.2 | 3318.1 | 3651.4 | +3165.5 | +3436.5 | 0/120 | true |
| pseudonymbench-container | pii | sse | 2 | 3245.3 | 3321.9 | +1.7 | -106.3 | 155.0 | 198.5 | +2.4 | -16.5 | 0/120 | true |
| direct | pii-long | sse | 2 | 3237.3 | 3270.4 | baseline | baseline | 153.0 | 156.7 | baseline | baseline | 0/120 | true |
| direct-repeat | pii-long | sse | 2 | 3235.1 | 3361.9 | -2.2 | +91.5 | 152.7 | 158.0 | -0.3 | +1.2 | 0/120 | true |
| litellm-presidio | pii-long | sse | 2 | 3359.8 | 3942.2 | +122.5 | +671.8 | 3359.8 | 3942.2 | +3206.9 | +3785.5 | 0/120 | true |
| pseudonymbench-container | pii-long | sse | 2 | 3236.0 | 3261.4 | -1.3 | -9.1 | 155.4 | 159.6 | +2.5 | +2.9 | 0/120 | true |

Δ is the difference of percentiles against the baseline run at the same payload, mode and rate. A row with `valid=false` had a load generator that fell behind its schedule; do not quote it.
