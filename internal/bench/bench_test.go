package bench

import (
	"path/filepath"
	"testing"
	"time"
)

func ms(n ...int) []time.Duration {
	out := make([]time.Duration, len(n))
	for i, v := range n {
		out[i] = time.Duration(v) * time.Millisecond
	}
	return out
}

func TestSummarizeNearestRank(t *testing.T) {
	var ds []time.Duration
	for i := 1; i <= 100; i++ {
		ds = append(ds, time.Duration(i)*time.Millisecond)
	}
	d := Summarize(ds)
	if d.N != 100 || d.P50 != 50 || d.P90 != 90 || d.P99 != 99 || d.P999 != 100 || d.Max != 100 || d.Mean != 50.5 {
		t.Errorf("dist = %+v", d)
	}
}

func TestSummarizeUnsortedAndTiny(t *testing.T) {
	d := Summarize(ms(30, 10, 20))
	if d.P50 != 20 || d.Max != 30 || d.P99 != 30 {
		t.Errorf("dist = %+v", d)
	}
	if one := Summarize(ms(7)); one.P50 != 7 || one.P99 != 7 {
		t.Errorf("single = %+v", one)
	}
	if empty := Summarize(nil); empty.N != 0 {
		t.Errorf("empty = %+v", empty)
	}
}

func TestResultRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	in := Result{Label: "x", Started: time.Unix(1700000000, 0).UTC(), Config: Config{Rate: 100, Stream: true},
		Summary: Summary{Requests: 5, TTFT: &Dist{N: 5, P50: 3}, Valid: true}}
	if err := in.Write(path); err != nil {
		t.Fatal(err)
	}
	out, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.Label != "x" || out.Config.Rate != 100 || out.Summary.TTFT == nil || out.Summary.TTFT.P50 != 3 {
		t.Errorf("round trip = %+v", out)
	}
}
