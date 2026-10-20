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

func TestCompact(t *testing.T) {
	mk := func(label string, stream bool, p50, p99, t50, t99 float64) bench.Result {
		r := result(label, stream, p50, p99, t50, t99)
		return r
	}
	out := compact([]bench.Result{
		mk("direct", false, 1000, 1010, 0, 0), mk("direct", true, 1100, 1200, 150, 160),
		mk("direct-repeat", false, 1001, 1012, 0, 0), mk("direct-repeat", true, 1101, 1203, 150.5, 163),
		mk("proxy", false, 1002, 1030, 0, 0), mk("proxy", true, 1103, 1230, 151, 170),
	}, "direct", "direct-repeat", "proxy")
	for _, want := range []string{"| pii |", "+2.0", "+20.0", "+1.0", "+10.0", "150.0 / 160.0", "1.0 / 2.0, 0.5 / 3.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("compact output lacks %q:\n%s", want, out)
		}
	}
}
