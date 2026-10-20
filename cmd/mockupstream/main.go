// Command mockupstream is a stand-in for an OpenAI-style chat-completions API.
// It exists so benchmarks measure the proxy, not the network or a real model:
// response timing is fixed and configurable, and the upstream can echo the
// prompt so the proxy has tokens to restore.
//
// It also records the request bodies it receives, keyed by the X-Eval-Id
// header, so the evaluation tools can see exactly what a proxy sent upstream.
package main

import (
	"container/list"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/token"
)

type config struct {
	firstToken time.Duration // delay before the first chunk (prefill)
	tokenDelay time.Duration // delay between chunks
	chunks     int           // reply length in chunks; fixed so timing never depends on the request
	chunkBytes int           // bytes of text per chunk
	echo       bool          // echo PII values or tokens found in the request
	keep       int           // recorded bodies kept
	overflow   *atomic.Int64 // replies whose echo exceeded the fixed length
}

type recorder struct {
	mu    sync.Mutex
	max   int
	order *list.List
	byID  map[string]*list.Element
}

type recorded struct{ id, body string }

func newRecorder(max int) *recorder {
	return &recorder{max: max, order: list.New(), byID: map[string]*list.Element{}}
}

func (r *recorder) put(id, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.byID[id]; ok {
		e.Value = recorded{id, body}
		return
	}
	r.byID[id] = r.order.PushBack(recorded{id, body})
	for r.order.Len() > r.max {
		old := r.order.Remove(r.order.Front()).(recorded)
		delete(r.byID, old.id)
	}
}

func (r *recorder) get(id string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e, ok := r.byID[id]; ok {
		return e.Value.(recorded).body, true
	}
	return "", false
}

func main() {
	var c config
	listen := flag.String("listen", ":9000", "address to listen on")
	flag.DurationVar(&c.firstToken, "first-token", 150*time.Millisecond, "delay before the first chunk")
	flag.DurationVar(&c.tokenDelay, "token-delay", 12*time.Millisecond, "delay between chunks")
	flag.IntVar(&c.chunks, "chunks", 240, "reply length in chunks; the reply is padded to exactly this size")
	flag.IntVar(&c.chunkBytes, "chunk-bytes", 4, "bytes of text per chunk (4 is roughly one BPE token)")
	flag.BoolVar(&c.echo, "echo", true, "echo every PII value or token found in the request, so a proxy has tokens to restore")
	flag.IntVar(&c.keep, "keep", 20000, "request bodies kept for /_received")
	flag.Parse()

	rec := newRecorder(c.keep)
	var requests atomic.Int64
	c.overflow = &atomic.Int64{}
	var lastMu sync.Mutex
	var lastBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok\n") })
	mux.HandleFunc("/_received/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/_received/")
		body, ok := rec.get(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	})
	// /_last returns the most recent request body. Proxies that do not forward
	// custom headers (so X-Eval-Id never arrives) can still be evaluated by
	// sending one request at a time and reading this back.
	mux.HandleFunc("/_last", func(w http.ResponseWriter, r *http.Request) {
		lastMu.Lock()
		b := lastBody
		lastMu.Unlock()
		if b == "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, b)
	})
	mux.HandleFunc("/_stats", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"requests":%d,"overflow":%d}`+"\n", requests.Load(), c.overflow.Load())
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests.Add(1)
		lastMu.Lock()
		lastBody = string(body)
		lastMu.Unlock()
		if id := r.Header.Get("X-Eval-Id"); id != "" {
			rec.put(id, string(body))
		}
		c.serve(w, r, body)
	})
	log.Printf("mockupstream: listening on %s (first token %v, %v/chunk, %d B/chunk)", *listen, c.firstToken, c.tokenDelay, c.chunkBytes)
	log.Fatal((&http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}).ListenAndServe())
}

// reply builds the response text. With echo on it lists every PII value (when
// the request came straight from a client) or token (when it came through a
// proxy) found in the request, then pads with filler to a fixed byte length.
// The fixed length keeps response timing identical whether or not a proxy sits
// in front, so latency differences are the proxy's own.
func (c config) reply(body []byte) string {
	budget := c.chunks * c.chunkBytes
	var sb strings.Builder
	sb.WriteString("Noted:")
	if c.echo {
		for _, item := range requestItems(body) {
			sb.WriteByte(' ')
			sb.WriteString(item)
		}
		sb.WriteByte('.')
	}
	if sb.Len() > budget {
		// The echo alone is longer than the fixed reply, so this reply is
		// longer than others and timing is no longer request-independent.
		c.overflow.Add(1)
		return sb.String()
	}
	for sb.Len() < budget {
		sb.WriteString(" lorem")
	}
	return sb.String()[:budget] // exactly budget bytes, so exactly the same chunk count every time
}

// requestItems walks the request JSON and returns, in order, the tokens and
// the detector matches found in its strings.
func requestItems(body []byte) []string {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return nil
	}
	var items []string
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			type hit struct {
				start int
				text  string
			}
			var hits []hit
			for _, loc := range token.Pattern.FindAllStringIndex(t, -1) {
				hits = append(hits, hit{loc[0], t[loc[0]:loc[1]]})
			}
			for _, m := range detect.Find(t, detect.AllKinds()) {
				hits = append(hits, hit{m.Start, m.Value(t)})
			}
			sort.Slice(hits, func(i, j int) bool { return hits[i].start < hits[j].start })
			for _, h := range hits {
				items = append(items, h.text)
			}
			if strings.HasPrefix(t, "{") { // tool-call arguments are JSON in a string
				var inner any
				if json.Unmarshal([]byte(t), &inner) == nil {
					walk(inner)
				}
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(t[k])
			}
		}
	}
	walk(v)
	return items
}

func (c config) serve(w http.ResponseWriter, r *http.Request, body []byte) {
	var req struct {
		Stream bool `json:"stream"`
		Model  string
	}
	json.Unmarshal(body, &req)
	if req.Model == "" {
		req.Model = "mock-model"
	}
	text := c.reply(body)
	chunks := splitBytes(text, c.chunkBytes)

	if !req.Stream {
		// A non-streamed reply is ready only after the whole generation.
		time.Sleep(c.firstToken + time.Duration(len(chunks))*c.tokenDelay)
		w.Header().Set("Content-Type", "application/json")
		out, _ := json.Marshal(map[string]any{
			"id": "chatcmpl-mock", "object": "chat.completion", "created": 1700000000, "model": req.Model,
			"choices": []any{map[string]any{"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": text}}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": len(chunks), "total_tokens": 10 + len(chunks)},
		})
		w.Write(out)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	fl := w.(http.Flusher)
	send := func(delta map[string]any, finish any) {
		b, _ := json.Marshal(map[string]any{
			"id": "chatcmpl-mock", "object": "chat.completion.chunk", "created": 1700000000, "model": req.Model,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		})
		fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}
	time.Sleep(c.firstToken)
	send(map[string]any{"role": "assistant", "content": ""}, nil)
	for i, ch := range chunks {
		if i > 0 {
			time.Sleep(c.tokenDelay)
		}
		select {
		case <-r.Context().Done():
			return
		default:
		}
		send(map[string]any{"content": ch}, nil)
	}
	send(map[string]any{}, "stop")
	io.WriteString(w, "data: [DONE]\n\n")
	fl.Flush()
}

// splitBytes cuts s into pieces of about n bytes, on rune boundaries.
func splitBytes(s string, n int) []string {
	if n <= 0 {
		return []string{s}
	}
	var out []string
	rs := []rune(s)
	var cur strings.Builder
	for _, r := range rs {
		if cur.Len() >= n {
			out = append(out, cur.String())
			cur.Reset()
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
