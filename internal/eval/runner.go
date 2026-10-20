package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/jsonx"
	"github.com/vikasgowda3007/pseudonymbench/internal/synth"
)

// Target is a proxy under test, plus the mock upstream behind it. The mock
// records every request body by X-Eval-Id, which is how the tools see what the
// proxy actually sent.
type Target struct {
	URL         string // chat-completions URL of the proxy (or of the mock, for a no-op control)
	MockURL     string // base URL of mockupstream
	Headers     map[string]string
	Concurrency int
	Client      *http.Client

	// Sequential sends one request at a time and reads the mock's /_last
	// instead of looking the body up by X-Eval-Id. Use it for proxies that do
	// not forward custom request headers to their upstream.
	Sequential bool
}

func (t Target) client() *http.Client {
	if t.Client != nil {
		return t.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// send posts body with an eval id and returns the status code.
func (t Target) send(ctx context.Context, id string, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Eval-Id", id)
	for k, v := range t.Headers {
		req.Header.Set(k, v)
	}
	resp, err := t.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

// received returns what the mock upstream saw for id.
func (t Target) received(ctx context.Context, id string) ([]byte, error) {
	u := strings.TrimRight(t.MockURL, "/") + "/_received/" + url.PathEscape(id)
	if t.Sequential {
		u = strings.TrimRight(t.MockURL, "/") + "/_last"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := t.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mock has no request %q (status %d)", id, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// LastUserContent returns the text of the last user message in a request body.
func LastUserContent(body []byte) (string, bool) {
	v, err := jsonx.Decode(body)
	if err != nil {
		return "", false
	}
	msgs, _ := v.(map[string]any)["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		m, _ := msgs[i].(map[string]any)
		if m["role"] != "user" {
			continue
		}
		switch c := m["content"].(type) {
		case string:
			return c, true
		case []any:
			var parts []string
			for _, p := range c {
				if pm, ok := p.(map[string]any); ok {
					if s, ok := pm["text"].(string); ok {
						parts = append(parts, s)
					}
				}
			}
			return strings.Join(parts, " "), true
		}
	}
	return "", false
}

// RunProxy sends every sample through the target and returns the regions each
// one had masked. errs[i] is true when the target rejected the request or the
// result could not be read.
func RunProxy(ctx context.Context, t Target, runID string, samples []synth.Sample) ([][]Region, []bool) {
	preds := make([][]Region, len(samples))
	errs := make([]bool, len(samples))
	workers := max(t.Concurrency, 1)
	if t.Sequential {
		workers = 1
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				id := fmt.Sprintf("%s-%d", runID, i)
				body, _ := json.Marshal(map[string]any{
					"model":    "eval-model",
					"messages": []any{map[string]any{"role": "user", "content": samples[i].Text}},
				})
				status, err := t.send(ctx, id, body)
				if err != nil || status != http.StatusOK {
					errs[i] = true
					continue
				}
				got, err := t.received(ctx, id)
				if err != nil {
					errs[i] = true
					continue
				}
				masked, ok := LastUserContent(got)
				if !ok {
					errs[i] = true
					continue
				}
				preds[i] = Regions(samples[i].Text, masked)
			}
		}()
	}
	for i := range samples {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return preds, errs
}

// DetectorRegions runs the detectors directly, with no proxy in between. It is
// the upper bound for what the regex tier can do on a dataset.
func DetectorRegions(samples []synth.Sample) [][]Region {
	out := make([][]Region, len(samples))
	for i, s := range samples {
		for _, m := range detect.Find(s.Text, detect.AllKinds()) {
			out[i] = append(out[i], Region{Start: m.Start, End: m.End, Kinds: []detect.Kind{m.Kind}, Replaced: 1})
		}
	}
	return out
}
