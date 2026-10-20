| setup | payload | mode | rps | total p50 (ms) | total p99 (ms) | Δp50 | Δp99 | TTFT p50 (ms) | TTFT p99 (ms) | ΔTTFT p50 | ΔTTFT p99 | errors | valid |
|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| direct | pii | json | 100 | 3043.7 | 3056.3 | baseline | baseline |  |  |  |  | 0/4000 | true |
| direct-repeat | pii | json | 100 | 3043.7 | 3056.3 | -0.0 | -0.0 |  |  |  |  | 0/4000 | true |
| pseudonymbench | pii | json | 100 | 3044.3 | 3045.6 | +0.5 | -10.8 |  |  |  |  | 0/4000 | true |
| direct | pii-long | json | 100 | 3044.7 | 3057.6 | baseline | baseline |  |  |  |  | 0/4000 | true |
| direct-repeat | pii-long | json | 100 | 3044.7 | 3057.4 | +0.0 | -0.1 |  |  |  |  | 0/4000 | true |
| pseudonymbench | pii-long | json | 100 | 3046.0 | 3047.4 | +1.3 | -10.1 |  |  |  |  | 0/4000 | true |
| direct | plain | json | 100 | 3043.6 | 3044.7 | baseline | baseline |  |  |  |  | 0/4000 | true |
| direct-repeat | plain | json | 100 | 3043.6 | 3045.0 | -0.0 | +0.2 |  |  |  |  | 0/4000 | true |
| pseudonymbench | plain | json | 100 | 3044.0 | 3045.5 | +0.4 | +0.8 |  |  |  |  | 0/4000 | true |
| direct | tools | json | 100 | 3044.0 | 3057.2 | baseline | baseline |  |  |  |  | 0/4000 | true |
| direct-repeat | tools | json | 100 | 3044.5 | 3058.0 | +0.6 | +0.8 |  |  |  |  | 0/4000 | true |
| pseudonymbench | tools | json | 100 | 3045.3 | 3047.2 | +1.3 | -10.0 |  |  |  |  | 0/4000 | true |
| direct | pii | sse | 100 | 3073.5 | 3213.2 | baseline | baseline | 150.9 | 153.4 | baseline | baseline | 0/4000 | true |
| direct-repeat | pii | sse | 100 | 3075.7 | 3241.3 | +2.2 | +28.1 | 150.9 | 153.8 | +0.0 | +0.3 | 0/4000 | true |
| pseudonymbench | pii | sse | 100 | 3071.5 | 3251.2 | -2.0 | +38.0 | 151.6 | 154.8 | +0.7 | +1.4 | 0/4000 | true |
| direct | pii-long | sse | 100 | 3072.4 | 3226.7 | baseline | baseline | 151.7 | 154.1 | baseline | baseline | 0/4000 | true |
| direct-repeat | pii-long | sse | 100 | 3072.1 | 3231.2 | -0.3 | +4.5 | 151.7 | 154.0 | -0.0 | -0.1 | 0/4000 | true |
| pseudonymbench | pii-long | sse | 100 | 3068.8 | 3244.3 | -3.6 | +17.5 | 151.9 | 154.8 | +0.2 | +0.7 | 0/4000 | true |
| direct | plain | sse | 100 | 3070.9 | 3201.8 | baseline | baseline | 150.8 | 152.6 | baseline | baseline | 0/4000 | true |
| direct-repeat | plain | sse | 100 | 3077.4 | 3224.8 | +6.5 | +23.1 | 150.9 | 155.4 | +0.2 | +2.8 | 0/4000 | true |
| pseudonymbench | plain | sse | 100 | 3072.0 | 3232.4 | +1.1 | +30.7 | 151.4 | 154.7 | +0.6 | +2.1 | 0/4000 | true |
| direct | tools | sse | 100 | 3126.6 | 3435.0 | baseline | baseline | 151.3 | 175.0 | baseline | baseline | 0/4000 | true |
| direct-repeat | tools | sse | 100 | 3119.6 | 3397.7 | -7.0 | -37.3 | 151.3 | 154.3 | +0.0 | -20.6 | 0/4000 | true |
| pseudonymbench | tools | sse | 100 | 3122.3 | 3415.3 | -4.2 | -19.6 | 151.8 | 155.9 | +0.5 | -19.1 | 0/4000 | true |

Δ is the difference of percentiles against the baseline run at the same payload, mode and rate. A row with `valid=false` had a load generator that fell behind its schedule; do not quote it.
machine: Apple M4 (10 cores), 16 GB, macOS 26.6, Go 1.26.3; mock upstream, proxy and load generator on one machine, no Docker; not a quiet benchmark host
