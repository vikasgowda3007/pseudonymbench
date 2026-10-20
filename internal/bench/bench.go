// Package bench holds the result format shared by the load generator, the
// evaluation tool and the report tool, plus the percentile arithmetic.
package bench

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"time"
)

// Dist summarises a set of durations in milliseconds.
type Dist struct {
	N    int     `json:"n"`
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P99  float64 `json:"p99"`
	P999 float64 `json:"p99_9"`
	Max  float64 `json:"max"`
}

// Summarize computes a Dist. Percentiles use the nearest-rank method on the
// sorted samples; with fewer than 1000 samples p99.9 is just the maximum, so
// callers should check N before quoting it.
func Summarize(ds []time.Duration) Dist {
	if len(ds) == 0 {
		return Dist{}
	}
	ms := make([]float64, len(ds))
	var sum float64
	for i, d := range ds {
		ms[i] = float64(d.Microseconds()) / 1000
		sum += ms[i]
	}
	sort.Float64s(ms)
	return Dist{
		N: len(ms), Mean: sum / float64(len(ms)),
		P50: pct(ms, 50), P90: pct(ms, 90), P99: pct(ms, 99), P999: pct(ms, 99.9), Max: ms[len(ms)-1],
	}
}

func pct(sorted []float64, p float64) float64 {
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	return sorted[min(max(rank, 1), len(sorted))-1]
}

// Config records how a run was driven, so a result file stands on its own.
type Config struct {
	URL        string        `json:"url"`
	Rate       float64       `json:"rate_rps"`
	Duration   time.Duration `json:"duration_ns"`
	Warmup     time.Duration `json:"warmup_ns"`
	Stream     bool          `json:"stream"`
	Payload    string        `json:"payload"`
	SessionPer int           `json:"session_per"`
	Timeout    time.Duration `json:"timeout_ns"`
}

// Env records the machine, because latency numbers are meaningless without it.
type Env struct {
	Go   string `json:"go"`
	OS   string `json:"os"`
	Arch string `json:"arch"`
	CPUs int    `json:"cpus"`
	Note string `json:"note,omitempty"`
}

// Summary is the outcome of one load run.
type Summary struct {
	Requests    int     `json:"requests"` // measured (after warmup)
	OK          int     `json:"ok"`
	Errors      int     `json:"errors"`
	Dropped     int     `json:"dropped"` // not sent because the in-flight cap was hit
	AchievedRPS float64 `json:"achieved_rps"`
	Total       Dist    `json:"total_ms"`
	TTFT        *Dist   `json:"ttft_ms,omitempty"` // streaming only: first content delta
	TTFB        Dist    `json:"ttfb_ms"`
	Lag         Dist    `json:"schedule_lag_ms"` // how late the generator sent versus the schedule
	// Valid is false when the generator itself fell behind its schedule, which
	// would make the latencies include client-side queueing.
	Valid bool `json:"valid"`
}

// Result is one file in results/.
type Result struct {
	Label   string    `json:"label"`
	Started time.Time `json:"started"`
	Config  Config    `json:"config"`
	Env     Env       `json:"env"`
	Summary Summary   `json:"summary"`
}

// Write saves r as indented JSON.
func (r Result) Write(path string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Read loads a result file.
func Read(path string) (Result, error) {
	var r Result
	b, err := os.ReadFile(path)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}
