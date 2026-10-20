| setup | payload | mode | rps | total p50 (ms) | total p99 (ms) | Δp50 | Δp99 | TTFT p50 (ms) | TTFT p99 (ms) | ΔTTFT p50 | ΔTTFT p99 | errors | valid |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| direct | pii | json | 100 | 3031.9 | 3035.4 | baseline | baseline |  |  |  |  | 0/2500 | true |
| direct-repeat | pii | json | 100 | 3031.6 | 3034.4 | -0.2 | -1.0 |  |  |  |  | 0/2500 | true |
| pseudonymbench | pii | json | 100 | 3032.3 | 3037.6 | +0.5 | +2.2 |  |  |  |  | 0/2500 | true |
| pseudonymbench-redis-hsetnx | pii | json | 100 | 3034.9 | 3064.7 | +3.0 | +29.3 |  |  |  |  | 0/2500 | true |
| pseudonymbench-redis-lua-concurrent | pii | json | 100 | 3033.9 | 3043.3 | +2.0 | +7.9 |  |  |  |  | 0/2500 | true |
| pseudonymbench-redis-lua | pii | json | 100 | 3033.4 | 3055.6 | +1.6 | +20.2 |  |  |  |  | 0/2500 | true |
| direct | pii-long | json | 100 | 3031.8 | 3034.1 | baseline | baseline |  |  |  |  | 0/2500 | true |
| direct-repeat | pii-long | json | 100 | 3031.9 | 3054.5 | +0.1 | +20.4 |  |  |  |  | 0/2500 | false |
| pseudonymbench | pii-long | json | 100 | 3032.6 | 3036.0 | +0.8 | +1.8 |  |  |  |  | 0/2500 | true |
| pseudonymbench-redis-hsetnx | pii-long | json | 100 | 3035.3 | 3053.9 | +3.5 | +19.7 |  |  |  |  | 0/2500 | true |
| pseudonymbench-redis-lua-concurrent | pii-long | json | 100 | 3035.6 | 3041.3 | +3.7 | +7.1 |  |  |  |  | 0/2500 | true |
| pseudonymbench-redis-lua | pii-long | json | 100 | 3034.4 | 3044.0 | +2.6 | +9.8 |  |  |  |  | 0/2500 | true |
| direct | pii | sse | 100 | 3077.4 | 3217.3 | baseline | baseline | 150.9 | 160.9 | baseline | baseline | 0/2500 | true |
| direct-repeat | pii | sse | 100 | 3076.2 | 3216.9 | -1.2 | -0.4 | 150.9 | 156.2 | +0.1 | -4.7 | 0/2500 | true |
| pseudonymbench | pii | sse | 100 | 3061.5 | 3234.4 | -15.9 | +17.1 | 151.3 | 158.9 | +0.4 | -2.0 | 0/2500 | true |
| pseudonymbench-redis-hsetnx | pii | sse | 100 | 3062.6 | 3240.4 | -14.9 | +23.1 | 153.7 | 168.0 | +2.8 | +7.0 | 0/2500 | true |
| pseudonymbench-redis-lua-concurrent | pii | sse | 100 | 3064.3 | 3223.1 | -13.1 | +5.8 | 152.5 | 158.0 | +1.6 | -2.9 | 0/2500 | true |
| pseudonymbench-redis-lua | pii | sse | 100 | 3063.2 | 3243.7 | -14.2 | +26.4 | 152.5 | 155.5 | +1.7 | -5.4 | 0/2500 | true |
| direct | pii-long | sse | 100 | 3125.9 | 3554.2 | baseline | baseline | 151.3 | 261.8 | baseline | baseline | 0/2500 | false |
| direct-repeat | pii-long | sse | 100 | 3147.1 | 3671.1 | +21.3 | +116.9 | 151.2 | 633.4 | -0.0 | +371.6 | 0/2500 | false |
| pseudonymbench | pii-long | sse | 100 | 3113.0 | 3384.0 | -12.9 | -170.2 | 151.8 | 156.3 | +0.6 | -105.5 | 0/2500 | true |
| pseudonymbench-redis-hsetnx | pii-long | sse | 100 | 3079.6 | 3611.5 | -46.2 | +57.3 | 156.2 | 685.4 | +4.9 | +423.6 | 0/2500 | false |
| pseudonymbench-redis-lua-concurrent | pii-long | sse | 100 | 3099.9 | 3413.9 | -25.9 | -140.3 | 155.0 | 311.4 | +3.8 | +49.5 | 0/2500 | true |
| pseudonymbench-redis-lua | pii-long | sse | 100 | 3107.7 | 3413.0 | -18.2 | -141.2 | 153.4 | 157.6 | +2.1 | -104.3 | 0/2500 | true |

Δ is the difference of percentiles against the baseline run at the same payload, mode and rate. A row with `valid=false` had a load generator that fell behind its schedule; do not quote it.
machine: Apple M4, macOS 26.6, Go go1.26.3; Redis 7.4 in Docker Desktop 29.8 (10 CPUs, 8 GB VM); proxy, mock, generator native
