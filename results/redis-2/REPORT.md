| setup | payload | mode | rps | total p50 (ms) | total p99 (ms) | Δp50 | Δp99 | TTFT p50 (ms) | TTFT p99 (ms) | ΔTTFT p50 | ΔTTFT p99 | errors | valid |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| direct | pii | json | 100 | 3032.4 | 3034.8 | baseline | baseline |  |  |  |  | 0/2500 | true |
| direct-repeat | pii | json | 100 | 3032.4 | 3034.6 | +0.0 | -0.2 |  |  |  |  | 0/2500 | true |
| pseudonymbench | pii | json | 100 | 3032.8 | 3035.6 | +0.4 | +0.9 |  |  |  |  | 0/2500 | true |
| pseudonymbench-redis-hsetnx | pii | json | 100 | 3035.0 | 3038.2 | +2.5 | +3.4 |  |  |  |  | 0/2500 | true |
| pseudonymbench-redis-lua | pii | json | 100 | 3033.9 | 3036.7 | +1.5 | +1.9 |  |  |  |  | 0/2500 | true |
| direct | pii-long | json | 100 | 3032.6 | 3034.8 | baseline | baseline |  |  |  |  | 0/2500 | true |
| direct-repeat | pii-long | json | 100 | 3032.6 | 3034.7 | +0.0 | -0.0 |  |  |  |  | 0/2500 | true |
| pseudonymbench | pii-long | json | 100 | 3033.2 | 3036.0 | +0.6 | +1.2 |  |  |  |  | 0/2500 | true |
| pseudonymbench-redis-hsetnx | pii-long | json | 100 | 3035.7 | 3038.7 | +3.2 | +3.9 |  |  |  |  | 0/2500 | true |
| pseudonymbench-redis-lua | pii-long | json | 100 | 3035.9 | 3039.7 | +3.3 | +4.9 |  |  |  |  | 0/2500 | true |
| direct | pii | sse | 100 | 3116.1 | 3373.9 | baseline | baseline | 151.1 | 154.4 | baseline | baseline | 0/2500 | true |
| direct-repeat | pii | sse | 100 | 3125.0 | 3367.9 | +8.9 | -5.9 | 151.1 | 154.6 | +0.0 | +0.2 | 0/2500 | true |
| pseudonymbench | pii | sse | 100 | 3119.1 | 3397.0 | +3.0 | +23.1 | 151.5 | 155.8 | +0.4 | +1.4 | 0/2500 | true |
| pseudonymbench-redis-hsetnx | pii | sse | 100 | 3098.3 | 3397.5 | -17.7 | +23.6 | 153.9 | 157.6 | +2.8 | +3.2 | 0/2500 | true |
| pseudonymbench-redis-lua | pii | sse | 100 | 3109.9 | 3391.8 | -6.2 | +17.9 | 152.9 | 156.5 | +1.8 | +2.1 | 0/2500 | true |
| direct | pii-long | sse | 100 | 3116.3 | 3360.0 | baseline | baseline | 151.2 | 155.3 | baseline | baseline | 0/2500 | true |
| direct-repeat | pii-long | sse | 100 | 3117.8 | 3352.9 | +1.5 | -7.1 | 151.3 | 155.7 | +0.1 | +0.4 | 0/2500 | true |
| pseudonymbench | pii-long | sse | 100 | 3108.6 | 3395.3 | -7.8 | +35.3 | 151.9 | 155.7 | +0.6 | +0.4 | 0/2500 | true |
| pseudonymbench-redis-hsetnx | pii-long | sse | 100 | 3077.7 | 3408.9 | -38.6 | +49.0 | 156.0 | 160.6 | +4.8 | +5.2 | 0/2500 | true |
| pseudonymbench-redis-lua | pii-long | sse | 100 | 3094.1 | 3403.9 | -22.2 | +43.9 | 154.8 | 158.3 | +3.6 | +3.0 | 0/2500 | true |

Δ is the difference of percentiles against the baseline run at the same payload, mode and rate. A row with `valid=false` had a load generator that fell behind its schedule; do not quote it.
machine: Apple M4, macOS 26.6, Go 1.26.3; Redis 7.4.11 in Docker Desktop (Linux VM, port-forwarded); mock, proxy and load generator native on the same machine
