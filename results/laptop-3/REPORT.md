| setup | payload | mode | rps | total p50 (ms) | total p99 (ms) | Δp50 | Δp99 | TTFT p50 (ms) | TTFT p99 (ms) | ΔTTFT p50 | ΔTTFT p99 | errors | valid |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| direct | pii | json | 100 | 3032.7 | 3034.9 | baseline | baseline |  |  |  |  | 0/3000 | true |
| direct-repeat | pii | json | 100 | 3032.5 | 3034.8 | -0.2 | -0.1 |  |  |  |  | 0/3000 | true |
| pseudonymbench | pii | json | 100 | 3033.1 | 3035.5 | +0.5 | +0.6 |  |  |  |  | 0/3000 | true |
| direct | pii-long | json | 100 | 3032.3 | 3034.5 | baseline | baseline |  |  |  |  | 0/3000 | true |
| direct-repeat | pii-long | json | 100 | 3032.4 | 3034.8 | +0.1 | +0.2 |  |  |  |  | 0/3000 | true |
| pseudonymbench | pii-long | json | 100 | 3033.0 | 3035.5 | +0.7 | +0.9 |  |  |  |  | 0/3000 | true |
| direct | plain | json | 100 | 3031.6 | 3033.1 | baseline | baseline |  |  |  |  | 0/3000 | true |
| direct-repeat | plain | json | 100 | 3032.6 | 3035.0 | +1.1 | +1.9 |  |  |  |  | 0/3000 | true |
| pseudonymbench | plain | json | 100 | 3033.0 | 3039.9 | +1.4 | +6.9 |  |  |  |  | 0/3000 | true |
| direct | tools | json | 100 | 3032.4 | 3034.7 | baseline | baseline |  |  |  |  | 0/3000 | true |
| direct-repeat | tools | json | 100 | 3032.6 | 3034.7 | +0.1 | +0.0 |  |  |  |  | 0/3000 | true |
| pseudonymbench | tools | json | 100 | 3032.8 | 3035.3 | +0.4 | +0.6 |  |  |  |  | 0/3000 | true |
| direct | pii | sse | 100 | 3142.8 | 3361.7 | baseline | baseline | 151.1 | 158.5 | baseline | baseline | 0/3000 | true |
| direct-repeat | pii | sse | 100 | 3117.5 | 3374.5 | -25.4 | +12.8 | 151.2 | 154.1 | +0.0 | -4.4 | 0/3000 | true |
| pseudonymbench | pii | sse | 100 | 3121.3 | 3399.6 | -21.5 | +37.8 | 151.6 | 156.0 | +0.5 | -2.5 | 0/3000 | true |
| direct | pii-long | sse | 100 | 3111.8 | 3389.7 | baseline | baseline | 151.2 | 154.3 | baseline | baseline | 0/3000 | true |
| direct-repeat | pii-long | sse | 100 | 3120.0 | 3372.2 | +8.2 | -17.4 | 151.1 | 154.5 | -0.1 | +0.2 | 0/3000 | true |
| pseudonymbench | pii-long | sse | 100 | 3114.1 | 3415.8 | +2.3 | +26.1 | 151.8 | 155.8 | +0.6 | +1.5 | 0/3000 | true |
| direct | plain | sse | 100 | 3123.7 | 3387.1 | baseline | baseline | 151.2 | 155.2 | baseline | baseline | 0/3000 | true |
| direct-repeat | plain | sse | 100 | 3123.3 | 3364.4 | -0.4 | -22.7 | 151.2 | 155.1 | +0.0 | -0.1 | 0/3000 | true |
| pseudonymbench | plain | sse | 100 | 3117.6 | 3408.4 | -6.1 | +21.3 | 151.6 | 155.9 | +0.4 | +0.7 | 0/3000 | true |
| direct | tools | sse | 100 | 3120.2 | 3410.7 | baseline | baseline | 151.2 | 154.6 | baseline | baseline | 0/3000 | true |
| direct-repeat | tools | sse | 100 | 3120.3 | 3378.7 | +0.1 | -32.0 | 151.3 | 154.6 | +0.0 | -0.0 | 0/3000 | true |
| pseudonymbench | tools | sse | 100 | 3117.9 | 3418.6 | -2.3 | +7.9 | 151.6 | 155.8 | +0.4 | +1.2 | 0/3000 | true |

Δ is the difference of percentiles against the baseline run at the same payload, mode and rate. A row with `valid=false` had a load generator that fell behind its schedule; do not quote it.
machine: Apple M4 (10 cores), 16 GB, macOS 26.6, Go 1.26.3; mock upstream, proxy and load generator on one machine, no Docker; not a quiet benchmark host
