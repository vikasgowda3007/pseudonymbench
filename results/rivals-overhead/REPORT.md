| setup | payload | mode | rps | total p50 (ms) | total p99 (ms) | Δp50 | Δp99 | TTFT p50 (ms) | TTFT p99 (ms) | ΔTTFT p50 | ΔTTFT p99 | errors | valid |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| direct | pii | json | 10 | 3034.0 | 3035.4 | baseline | baseline |  |  |  |  | 0/400 | true |
| direct-repeat | pii | json | 10 | 3034.1 | 3035.4 | +0.1 | +0.0 |  |  |  |  | 0/400 | true |
| litellm-presidio | pii | json | 10 | 3066.8 | 3085.6 | +32.8 | +50.2 |  |  |  |  | 0/400 | true |
| pseudonymbench-container | pii | json | 10 | 3037.1 | 3039.3 | +3.1 | +3.9 |  |  |  |  | 0/400 | true |
| direct | pii-long | json | 10 | 3034.4 | 3035.6 | baseline | baseline |  |  |  |  | 0/400 | true |
| direct-repeat | pii-long | json | 10 | 3034.4 | 3035.6 | +0.0 | +0.0 |  |  |  |  | 0/400 | true |
| litellm-presidio | pii-long | json | 10 | 3103.7 | 3146.4 | +69.3 | +110.9 |  |  |  |  | 0/400 | true |
| pseudonymbench-container | pii-long | json | 10 | 3037.5 | 3041.0 | +3.2 | +5.5 |  |  |  |  | 0/400 | true |
| direct | pii | sse | 10 | 3315.5 | 3387.4 | baseline | baseline | 153.0 | 155.7 | baseline | baseline | 0/400 | true |
| direct-repeat | pii | sse | 10 | 3311.9 | 3389.5 | -3.6 | +2.1 | 153.1 | 155.4 | +0.1 | -0.3 | 0/400 | true |
| litellm-presidio | pii | sse | 10 | 3395.4 | 5080.7 | +79.9 | +1693.3 | 3395.4 | 5080.7 | +3242.4 | +4925.0 | 0/400 | true |
| pseudonymbench-container | pii | sse | 10 | 3319.4 | 3372.9 | +3.8 | -14.4 | 155.5 | 162.9 | +2.5 | +7.2 | 0/400 | true |
| direct | pii-long | sse | 10 | 3311.3 | 3405.7 | baseline | baseline | 153.2 | 155.9 | baseline | baseline | 0/400 | true |
| direct-repeat | pii-long | sse | 10 | 3320.1 | 3411.0 | +8.8 | +5.3 | 153.3 | 155.7 | +0.0 | -0.2 | 0/400 | true |
| litellm-presidio | pii-long | sse | 10 | 6372.9 | 8718.4 | +3061.6 | +5312.7 | 6372.9 | 8718.4 | +6219.6 | +8562.4 | 0/400 | true |
| pseudonymbench-container | pii-long | sse | 10 | 3317.8 | 3395.8 | +6.5 | -9.9 | 156.1 | 162.9 | +2.8 | +7.0 | 0/400 | true |

Δ is the difference of percentiles against the baseline run at the same payload, mode and rate. A row with `valid=false` had a load generator that fell behind its schedule; do not quote it.
machine: Apple M4, macOS 26.6, Docker Desktop 29.8 (10 CPUs, 8 GB VM); pseudonymbench, LiteLLM, Presidio in containers; mock and load generator native on the host
