// Command loadgen drives an OpenAI-style endpoint at a fixed arrival rate and
// records latency and, for streams, time to first token.
//
// It is open-loop: request i is scheduled for start + i/rate no matter how
// slowly earlier requests finish, and latency is measured from that intended
// start time, not from when the generator got around to sending. A closed-loop
// client slows down when the server does, which hides exactly the queueing the
// benchmark exists to expose (coordinated omission; see the wrk2 README).
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptrace"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vikasgowda3007/pseudonymbench/internal/bench"
	"github.com/vikasgowda3007/pseudonymbench/internal/synth"
)

type sample struct {
	intended time.Time
	lag      time.Duration
	ttfb     time.Duration
	ttft     time.Duration // 0 when not streaming or no content arrived
	total    time.Duration
	ok       bool
}

func main() {
	var (
		url        = flag.String("url", "http://localhost:8080/v1/chat/completions", "endpoint to drive")
		rate       = flag.Float64("rate", 100, "arrival rate, requests per second")
		duration   = flag.Duration("duration", 30*time.Second, "measured duration (after warmup)")
		warmup     = flag.Duration("warmup", 5*time.Second, "warmup before measuring; requests are sent but not counted")
		stream     = flag.Bool("stream", false, "request streamed responses and measure time to first token")
		payload    = flag.String("payload", "pii", "request shape: "+strings.Join(synth.PayloadSets, ", "))
		pool       = flag.Int("pool", 20000, "distinct request bodies generated, reused cyclically")
		sessionPer = flag.Int("session-per", 0, "if > 0, requests share a session header in groups of this size")
		auth       = flag.String("auth", "Bearer sk-bench", "Authorization header value")
		timeout    = flag.Duration("timeout", 30*time.Second, "per-request timeout")
		maxInfl    = flag.Int("max-inflight", 20000, "requests in flight before new ones are dropped")
		label      = flag.String("label", "", "name of this configuration in the result file")
		out        = flag.String("out", "", "result JSON path (default: print only)")
		note       = flag.String("note", "", "free text stored in the result, e.g. the machine setup")
		seed       = flag.Int64("seed", 1, "seed for generated request bodies")
	)
	flag.Parse()
	if *label == "" {
		*label = *payload
	}

	g := synth.New(*seed)
	bodies := make([][]byte, *pool)
	for i := range bodies {
		b, err := g.ChatBody(*payload, *stream)
		if err != nil {
			log.Fatal(err)
		}
		bodies[i] = b
	}

	client := &http.Client{Transport: &http.Transport{
		MaxIdleConns: *maxInfl, MaxIdleConnsPerHost: *maxInfl, IdleConnTimeout: 90 * time.Second,
		DisableCompression: true,
	}}

	var (
		mu       sync.Mutex
		samples  []sample
		inflight atomic.Int64
		dropped  atomic.Int64
		wg       sync.WaitGroup
	)
	start := time.Now().Add(200 * time.Millisecond)
	measureFrom := start.Add(*warmup)
	end := measureFrom.Add(*duration)
	interval := time.Duration(float64(time.Second) / *rate)

	fire := func(i int, intended time.Time) {
		defer wg.Done()
		defer inflight.Add(-1)
		s := sample{intended: intended, lag: time.Since(intended)}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, *url, bytes.NewReader(bodies[i%len(bodies)]))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", *auth)
		if *sessionPer > 0 {
			req.Header.Set("X-Pseudonym-Session", fmt.Sprintf("bench-%d", i / *sessionPer))
		}
		// The trace callback runs on the transport's goroutine, so hand its
		// result over atomically (a duration is always > 0, so 0 means unset).
		var firstByte atomic.Int64
		trace := &httptrace.ClientTrace{GotFirstResponseByte: func() { firstByte.Store(int64(time.Since(intended))) }}
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

		resp, err := client.Do(req)
		if err == nil {
			s.ok = resp.StatusCode == http.StatusOK
			if *stream && s.ok {
				s.ttft = readStream(resp.Body, intended)
			} else {
				io.Copy(io.Discard, resp.Body)
			}
			resp.Body.Close()
		}
		s.total = time.Since(intended)
		s.ttfb = time.Duration(firstByte.Load())
		if s.ok && *stream && s.ttft == 0 {
			s.ok = false // a stream that never produced content counts as a failure
		}
		if !intended.Before(measureFrom) {
			mu.Lock()
			samples = append(samples, s)
			mu.Unlock()
		}
	}

	log.Printf("loadgen: %s at %.0f rps for %v (+%v warmup), payload=%s stream=%v", *url, *rate, *duration, *warmup, *payload, *stream)
	for i := 0; ; i++ {
		intended := start.Add(time.Duration(i) * interval)
		if !intended.Before(end) {
			break
		}
		// Sleep until due; if already late, send immediately and keep the schedule.
		if d := time.Until(intended); d > 0 {
			time.Sleep(d)
		}
		if inflight.Load() >= int64(*maxInfl) {
			if !intended.Before(measureFrom) {
				dropped.Add(1)
			}
			continue
		}
		inflight.Add(1)
		wg.Add(1)
		go fire(i, intended)
	}
	wg.Wait()

	res := summarize(samples, int(dropped.Load()), *duration, *stream)
	r := bench.Result{
		Label: *label, Started: start,
		Config: bench.Config{URL: *url, Rate: *rate, Duration: *duration, Warmup: *warmup, Stream: *stream,
			Payload: *payload, SessionPer: *sessionPer, Timeout: *timeout},
		Env:     bench.Env{Go: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, CPUs: runtime.NumCPU(), Note: *note},
		Summary: res,
	}
	printSummary(r)
	if *out != "" {
		if err := r.Write(*out); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", *out)
	}
	if !res.Valid {
		os.Exit(2)
	}
}

// readStream consumes an SSE body and returns the time, measured from
// intended, at which the first content delta arrived (0 if none did).
func readStream(body io.Reader, intended time.Time) time.Duration {
	var ttft time.Duration
	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			break
		}
		if ttft != 0 || !strings.Contains(data, `"content"`) {
			continue
		}
		var c struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if json.Unmarshal([]byte(data), &c) == nil {
			for _, ch := range c.Choices {
				if ch.Delta.Content != "" {
					ttft = time.Since(intended)
				}
			}
		}
	}
	return ttft
}

func summarize(samples []sample, dropped int, dur time.Duration, streaming bool) bench.Summary {
	var total, ttfb, ttft, lag []time.Duration
	s := bench.Summary{Requests: len(samples), Dropped: dropped}
	for _, x := range samples {
		if x.ok {
			s.OK++
			total = append(total, x.total)
			ttfb = append(ttfb, x.ttfb)
			if streaming {
				ttft = append(ttft, x.ttft)
			}
		} else {
			s.Errors++
		}
		lag = append(lag, x.lag)
	}
	s.AchievedRPS = float64(len(samples)) / dur.Seconds()
	s.Total, s.TTFB, s.Lag = bench.Summarize(total), bench.Summarize(ttfb), bench.Summarize(lag)
	if streaming {
		d := bench.Summarize(ttft)
		s.TTFT = &d
	}
	// The run is trustworthy only if the generator kept its schedule.
	s.Valid = s.Lag.P99 <= 5 && dropped == 0 && s.Requests > 0
	return s
}

func printSummary(r bench.Result) {
	s := r.Summary
	fmt.Printf("%-24s n=%d ok=%d err=%d drop=%d  total p50=%.1f p99=%.1f ms", r.Label, s.Requests, s.OK, s.Errors, s.Dropped, s.Total.P50, s.Total.P99)
	if s.TTFT != nil {
		fmt.Printf("  ttft p50=%.1f p99=%.1f ms", s.TTFT.P50, s.TTFT.P99)
	}
	fmt.Printf("  lag p99=%.2f ms  valid=%v\n", s.Lag.P99, s.Valid)
}
