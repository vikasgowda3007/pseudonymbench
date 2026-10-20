// Command benchreport turns load-generator result files into a Markdown table
// of overhead against a baseline (normally a run straight to the mock
// upstream at the same rate and payload).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/bench"
)

type key struct {
	payload string
	stream  bool
	rate    float64
}

func main() {
	baseline := flag.String("baseline", "direct", "label of the baseline runs")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: benchreport [-baseline direct] results/*.json\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	var results []bench.Result
	for _, p := range flag.Args() {
		r, err := bench.Read(p)
		if err != nil {
			log.Fatal(err)
		}
		results = append(results, r)
	}
	fmt.Print(render(results, *baseline))
}

func render(results []bench.Result, baseline string) string {
	base := map[key]bench.Result{}
	for _, r := range results {
		if r.Label == baseline {
			base[keyOf(r)] = r
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		a, b := keyOf(results[i]), keyOf(results[j])
		if a.stream != b.stream {
			return !a.stream
		}
		if a.payload != b.payload {
			return a.payload < b.payload
		}
		if a.rate != b.rate {
			return a.rate < b.rate
		}
		return results[i].Label == baseline && results[j].Label != baseline
	})

	var sb strings.Builder
	sb.WriteString("| setup | payload | mode | rps | total p50 (ms) | total p99 (ms) | Δp50 | Δp99 | TTFT p50 (ms) | TTFT p99 (ms) | ΔTTFT p50 | ΔTTFT p99 | errors | valid |\n")
	sb.WriteString("|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
	for _, r := range results {
		k := keyOf(r)
		b, hasBase := base[k]
		s := r.Summary
		mode := "json"
		if k.stream {
			mode = "sse"
		}
		d50, d99, dt50, dt99 := "n/a", "n/a", "n/a", "n/a"
		ttft50, ttft99 := "", ""
		if s.TTFT != nil {
			ttft50, ttft99 = f1(s.TTFT.P50), f1(s.TTFT.P99)
		}
		if r.Label == baseline {
			d50, d99, dt50, dt99 = "baseline", "baseline", "baseline", "baseline"
			if s.TTFT == nil {
				dt50, dt99 = "", ""
			}
		} else if hasBase {
			d50, d99 = delta(s.Total.P50, b.Summary.Total.P50), delta(s.Total.P99, b.Summary.Total.P99)
			if s.TTFT != nil && b.Summary.TTFT != nil {
				dt50, dt99 = delta(s.TTFT.P50, b.Summary.TTFT.P50), delta(s.TTFT.P99, b.Summary.TTFT.P99)
			} else {
				dt50, dt99 = "", ""
			}
		} else if s.TTFT == nil {
			dt50, dt99 = "", ""
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %.0f | %s | %s | %s | %s | %s | %s | %s | %s | %d/%d | %v |\n",
			r.Label, k.payload, mode, k.rate, f1(s.Total.P50), f1(s.Total.P99), d50, d99,
			ttft50, ttft99, dt50, dt99, s.Errors, s.Requests, s.Valid)
	}
	sb.WriteString("\nΔ is the difference of percentiles against the baseline run at the same payload, mode and rate. ")
	sb.WriteString("A row with `valid=false` had a load generator that fell behind its schedule; do not quote it.\n")
	return sb.String()
}

func keyOf(r bench.Result) key {
	return key{payload: r.Config.Payload, stream: r.Config.Stream, rate: r.Config.Rate}
}

func f1(v float64) string { return fmt.Sprintf("%.1f", v) }

func delta(v, base float64) string { return fmt.Sprintf("%+.1f", v-base) }
