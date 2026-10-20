package main

import (
	"strings"
	"testing"

	"github.com/vikasgowda3007/pseudonymbench/internal/bench"
)

func result(label string, stream bool, p50, p99, t50, t99 float64) bench.Result {
	r := bench.Result{Label: label, Config: bench.Config{Payload: "pii", Rate: 100, Stream: stream},
		Summary: bench.Summary{Requests: 10, Valid: true, Total: bench.Dist{P50: p50, P99: p99}}}
	if stream {
		r.Summary.TTFT = &bench.Dist{P50: t50, P99: t99}
	}
	return r
}

func TestRenderDeltas(t *testing.T) {
	out := render([]bench.Result{
		result("proxy", true, 1100, 1200, 160, 190),
		result("direct", true, 1000, 1050, 150, 160),
	}, "direct")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if !strings.Contains(lines[2], "| direct |") || !strings.Contains(lines[2], "baseline") {
		t.Errorf("baseline row should come first:\n%s", out)
	}
	if !strings.Contains(lines[3], "| proxy |") || !strings.Contains(lines[3], "+100.0") || !strings.Contains(lines[3], "+150.0") ||
		!strings.Contains(lines[3], "+10.0") || !strings.Contains(lines[3], "+30.0") {
		t.Errorf("proxy row deltas wrong:\n%s", lines[3])
	}
}

func TestRenderWithoutBaseline(t *testing.T) {
	out := render([]bench.Result{result("proxy", false, 10, 20, 0, 0)}, "direct")
	if !strings.Contains(out, "n/a") {
		t.Errorf("missing baseline should show n/a:\n%s", out)
	}
}
