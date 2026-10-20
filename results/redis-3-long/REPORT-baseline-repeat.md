| setup | payload | mode | rps | total p50 (ms) | total p99 (ms) | Δp50 | Δp99 | TTFT p50 (ms) | TTFT p99 (ms) | ΔTTFT p50 | ΔTTFT p99 | errors | valid |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| direct-repeat | pii-long | json | 100 | 3032.0 | 3101.2 | baseline | baseline |  |  |  |  | 0/2500 | true |
| direct | pii-long | json | 100 | 3031.9 | 3089.4 | -0.1 | -11.8 |  |  |  |  | 0/2500 | false |
| pseudonymbench | pii-long | json | 100 | 3032.8 | 3224.0 | +0.8 | +122.8 |  |  |  |  | 0/2500 | false |
| pseudonymbench-redis-hsetnx | pii-long | json | 100 | 3035.5 | 3101.0 | +3.4 | -0.2 |  |  |  |  | 0/2500 | true |
| pseudonymbench-redis-lua-concurrent | pii-long | json | 100 | 3035.4 | 3043.2 | +3.4 | -58.0 |  |  |  |  | 0/2500 | true |
| pseudonymbench-redis-lua | pii-long | json | 100 | 3034.6 | 3072.9 | +2.6 | -28.3 |  |  |  |  | 0/2500 | true |
| direct-repeat | pii-long | sse | 100 | 3072.3 | 3213.1 | baseline | baseline | 151.0 | 156.1 | baseline | baseline | 0/2500 | true |
| direct | pii-long | sse | 100 | 3084.3 | 3357.4 | +12.0 | +144.3 | 151.0 | 190.6 | -0.0 | +34.5 | 0/2500 | false |
| pseudonymbench | pii-long | sse | 100 | 3069.5 | 3248.5 | -2.8 | +35.4 | 151.5 | 160.6 | +0.5 | +4.5 | 0/2500 | true |
| pseudonymbench-redis-hsetnx | pii-long | sse | 100 | 3058.8 | 3234.6 | -13.5 | +21.6 | 155.1 | 165.2 | +4.1 | +9.1 | 0/2500 | true |
| pseudonymbench-redis-lua-concurrent | pii-long | sse | 100 | 3061.0 | 3227.7 | -11.4 | +14.7 | 154.4 | 158.3 | +3.4 | +2.2 | 0/2500 | true |
| pseudonymbench-redis-lua | pii-long | sse | 100 | 3064.7 | 3242.0 | -7.6 | +28.9 | 153.0 | 159.0 | +2.0 | +2.9 | 0/2500 | true |

Δ is the difference of percentiles against the baseline run at the same payload, mode and rate. A row with `valid=false` had a load generator that fell behind its schedule; do not quote it.
Baseline: direct-repeat, because the first direct run in this directory was flagged invalid (generator lag); the original REPORT.md uses direct.
