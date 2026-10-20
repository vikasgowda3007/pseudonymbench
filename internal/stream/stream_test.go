package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/jsonx"
	"github.com/vikasgowda3007/pseudonymbench/internal/mask"
	"github.com/vikasgowda3007/pseudonymbench/internal/vault"
)

var ctx = context.Background()

// --- helpers ---------------------------------------------------------------

type fixture struct {
	sess        *mask.Session
	email, card string // tokens
}

func newFixture(t testing.TB) fixture {
	t.Helper()
	sess := mask.NewSession(vault.NewMemory(vault.MemoryOptions{}), "s1")
	e, err := sess.Tokenize(ctx, detect.Email, "john@acme.com")
	if err != nil {
		t.Fatal(err)
	}
	c, _ := sess.Tokenize(ctx, detect.Card, "4111 1111 1111 1111")
	return fixture{sess: sess, email: e, card: c}
}

// chunk renders one chat.completion.chunk event.
func chunk(choice int, delta map[string]any, finish any) string {
	return event(map[string]any{
		"id": "chatcmpl-1", "object": "chat.completion.chunk", "created": 1700000000, "model": "gpt-test",
		"choices": []any{map[string]any{"index": choice, "delta": delta, "finish_reason": finish}},
	})
}

func event(v any) string {
	b, err := jsonx.Encode(v)
	if err != nil {
		panic(err)
	}
	return "data: " + string(b) + "\n\n"
}

const done = "data: [DONE]\n\n"

func contentChunk(choice int, s string) string {
	return chunk(choice, map[string]any{"content": s}, nil)
}

// rewrite runs body through a Rewriter and returns the output and stats.
func rewrite(t testing.TB, f fixture, body string) (string, Stats) {
	t.Helper()
	rw := New(ctx, io.NopCloser(strings.NewReader(body)), f.sess, Options{})
	out, err := io.ReadAll(rw)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(out), rw.Stats()
}

type outEvent struct {
	raw  string
	done bool
	json map[string]any
}

func parseOut(t testing.TB, out string) []outEvent {
	t.Helper()
	var evs []outEvent
	for _, raw := range strings.SplitAfter(out, "\n\n") {
		if raw == "" {
			continue
		}
		e := outEvent{raw: raw}
		for _, line := range strings.Split(raw, "\n") {
			if rest, ok := strings.CutPrefix(line, "data:"); ok {
				rest = strings.TrimPrefix(rest, " ")
				if rest == "[DONE]" {
					e.done = true
					continue
				}
				v, err := jsonx.Decode([]byte(rest))
				if err != nil {
					t.Fatalf("output event is not valid JSON: %v\n%s", err, raw)
				}
				e.json, _ = v.(map[string]any)
			}
		}
		evs = append(evs, e)
	}
	return evs
}

// texts concatenates, per choice, the content and refusal fields of an output.
func contentByChoice(evs []outEvent) map[int]string {
	m := map[int]string{}
	for _, e := range evs {
		if e.json == nil {
			continue
		}
		cs, _ := e.json["choices"].([]any)
		for _, c := range cs {
			cm := c.(map[string]any)
			idx := intOf(cm["index"], 0)
			if d, ok := cm["delta"].(map[string]any); ok {
				if s, ok := d["content"].(string); ok {
					m[idx] += s
				}
			}
		}
	}
	return m
}

// argsByTool concatenates tool-call argument fragments per (choice, tool).
func argsByTool(evs []outEvent) map[[2]int]string {
	m := map[[2]int]string{}
	for _, e := range evs {
		if e.json == nil {
			continue
		}
		cs, _ := e.json["choices"].([]any)
		for _, c := range cs {
			cm := c.(map[string]any)
			idx := intOf(cm["index"], 0)
			d, _ := cm["delta"].(map[string]any)
			tcs, _ := d["tool_calls"].([]any)
			for i, tc := range tcs {
				tm := tc.(map[string]any)
				fn, _ := tm["function"].(map[string]any)
				if s, ok := fn["arguments"].(string); ok {
					m[[2]int{idx, intOf(tm["index"], i)}] += s
				}
			}
		}
	}
	return m
}

func cutAt(s string, cuts []int) []string {
	sort.Ints(cuts)
	var parts []string
	prev := 0
	for _, c := range cuts {
		if c > prev && c <= len(s) {
			parts = append(parts, s[prev:c])
			prev = c
		}
	}
	return append(parts, s[prev:])
}

func streamOf(choice int, parts []string, finish string) string {
	var b strings.Builder
	b.WriteString(chunk(choice, map[string]any{"role": "assistant", "content": ""}, nil))
	for _, p := range parts {
		b.WriteString(contentChunk(choice, p))
	}
	b.WriteString(chunk(choice, map[string]any{}, finish))
	b.WriteString(done)
	return b.String()
}

// --- tests -----------------------------------------------------------------

func TestRestoresTokenSplitAtEveryPosition(t *testing.T) {
	f := newFixture(t)
	text := "Sent to " + f.email + " and charged " + f.card + ". Done <3"
	want := "Sent to john@acme.com and charged 4111 1111 1111 1111. Done <3"

	check := func(parts []string) {
		out, _ := rewrite(t, f, streamOf(0, parts, "stop"))
		if got := contentByChoice(parseOut(t, out))[0]; got != want {
			t.Fatalf("parts %q\n got  %q\n want %q", parts, got, want)
		}
	}
	// Every single split and every pair of splits.
	for i := 0; i <= len(text); i++ {
		check(cutAt(text, []int{i}))
		for j := i + 1; j <= len(text); j++ {
			check(cutAt(text, []int{i, j}))
		}
	}
	// Character by character.
	cuts := make([]int, len(text))
	for i := range cuts {
		cuts[i] = i + 1
	}
	check(cutAt(text, cuts))
}

func TestRandomChunking(t *testing.T) {
	f := newFixture(t)
	rng := rand.New(rand.NewSource(1))
	pieces := []string{f.email, f.card, "plain words ", "<", ">", "<div>", "<EMAIL_", "<EMAIL_zz", " a < b ", "https://x.test/?q=", "\n"}
	for iter := 0; iter < 3000; iter++ {
		var tb strings.Builder
		for n := rng.Intn(8) + 1; n > 0; n-- {
			tb.WriteString(pieces[rng.Intn(len(pieces))])
		}
		text := tb.String()
		var cuts []int
		for n := rng.Intn(6); n > 0; n-- {
			cuts = append(cuts, rng.Intn(len(text)+1))
		}
		out, _ := rewrite(t, f, streamOf(0, cutAt(text, cuts), "stop"))
		want := (&mask.Restorer{Session: f.sess}).Text(ctx, text)
		if got := contentByChoice(parseOut(t, out))[0]; got != want {
			t.Fatalf("iter %d cuts %v\n text %q\n got  %q\n want %q", iter, cuts, text, got, want)
		}
	}
}

func FuzzChunking(f *testing.F) {
	fx := newFixture(f)
	f.Add("hi "+fx.email+" bye", []byte{3, 9, 14})
	f.Add("<EMAIL_<EMAIL_"+fx.card, []byte{1, 2, 3, 4, 5})
	f.Add("a < b > c", []byte{})
	f.Fuzz(func(t *testing.T, text string, cutBytes []byte) {
		if !utf8.ValidString(text) {
			return
		}
		var cuts []int
		for _, c := range cutBytes {
			p := int(c) % (len(text) + 1)
			for p < len(text) && !utf8.RuneStart(text[p]) {
				p++
			}
			cuts = append(cuts, p)
		}
		out, _ := rewrite(t, fx, streamOf(0, cutAt(text, cuts), "stop"))
		want := (&mask.Restorer{Session: fx.sess}).Text(ctx, text)
		if got := contentByChoice(parseOut(t, out))[0]; got != want {
			t.Fatalf("cuts %v\n text %q\n got  %q\n want %q", cuts, text, got, want)
		}
	})
}

func TestHeldTextIsFlushedBeforeFinishChunk(t *testing.T) {
	f := newFixture(t)
	out, _ := rewrite(t, f, streamOf(0, []string{"see <EM"}, "length"))
	evs := parseOut(t, out)
	finishAt, textAt := -1, -1
	for i, e := range evs {
		if e.json == nil {
			continue
		}
		c := e.json["choices"].([]any)[0].(map[string]any)
		if c["finish_reason"] == "length" {
			finishAt = i
		}
		if d, _ := c["delta"].(map[string]any); d != nil {
			if s, _ := d["content"].(string); strings.Contains(s, "<EM") {
				textAt = i
			}
		}
	}
	if textAt < 0 || finishAt < 0 || textAt > finishAt {
		t.Fatalf("held text at event %d, finish at %d; tail must not follow the finish chunk\n%s", textAt, finishAt, out)
	}
	if got := contentByChoice(evs)[0]; got != "see <EM" {
		t.Errorf("content = %q", got)
	}
	if !evs[len(evs)-1].done {
		t.Error("[DONE] is not last")
	}
}

func TestFlushOnDoneWithoutFinishReason(t *testing.T) {
	f := newFixture(t)
	body := chunk(0, map[string]any{"content": "tail <EMA"}, nil) + done
	out, _ := rewrite(t, f, body)
	evs := parseOut(t, out)
	if got := contentByChoice(evs)[0]; got != "tail <EMA" {
		t.Errorf("content = %q", got)
	}
	if !evs[len(evs)-1].done || len(evs) < 3 {
		t.Errorf("want content chunk, synthetic flush chunk, [DONE]; got %d events:\n%s", len(evs), out)
	}
	// Synthetic chunk copies the stream's metadata.
	syn := evs[len(evs)-2].json
	if syn["id"] != "chatcmpl-1" || syn["model"] != "gpt-test" || syn["object"] != "chat.completion.chunk" {
		t.Errorf("synthetic chunk lacks metadata: %v", syn)
	}
}

func TestFlushOnEOFWithoutDone(t *testing.T) {
	f := newFixture(t)
	out, _ := rewrite(t, f, chunk(0, map[string]any{"content": "cut off <CARD_41"}, nil))
	if got := contentByChoice(parseOut(t, out))[0]; got != "cut off <CARD_41" {
		t.Errorf("content = %q", got)
	}
}

func TestFlushBeforeErrorFrame(t *testing.T) {
	f := newFixture(t)
	body := chunk(0, map[string]any{"content": "oops <EM"}, nil) +
		`data: {"error":{"message":"upstream failed"}}` + "\n\n"
	out, _ := rewrite(t, f, body)
	evs := parseOut(t, out)
	if len(evs) != 3 {
		t.Fatalf("want content, flush, error; got %d events:\n%s", len(evs), out)
	}
	if _, isErr := evs[2].json["error"]; !isErr {
		t.Errorf("error frame is not last: %s", out)
	}
	if got := contentByChoice(evs)[0]; got != "oops <EM" {
		t.Errorf("content = %q", got)
	}
}

func TestInterleavedChoices(t *testing.T) {
	f := newFixture(t)
	other, _ := f.sess.Tokenize(ctx, detect.Phone, "415-555-2671")
	a := "A: " + f.email + " end"
	b := "B: " + other + " end"
	ac, bc := cutAt(a, []int{5, 9, 14}), cutAt(b, []int{4, 8, 13})
	var body strings.Builder
	for i := 0; i < max(len(ac), len(bc)); i++ {
		if i < len(ac) {
			body.WriteString(contentChunk(0, ac[i]))
		}
		if i < len(bc) {
			body.WriteString(contentChunk(1, bc[i]))
		}
	}
	body.WriteString(chunk(0, map[string]any{}, "stop") + chunk(1, map[string]any{}, "stop") + done)
	out, _ := rewrite(t, f, body.String())
	got := contentByChoice(parseOut(t, out))
	if got[0] != "A: john@acme.com end" || got[1] != "B: 415-555-2671 end" {
		t.Errorf("choices cross-contaminated: %v", got)
	}
}

func TestToolCallArgumentsSplitAndParallel(t *testing.T) {
	f := newFixture(t)
	toolB, _ := f.sess.Tokenize(ctx, detect.Email, "jane@acme.com")
	frags := func(idx int, id, name string, args []string) []string {
		var evs []string
		for i, a := range args {
			tc := map[string]any{"index": idx, "function": map[string]any{"arguments": a}}
			if i == 0 {
				tc["id"], tc["type"] = id, "function"
				tc["function"].(map[string]any)["name"] = name
			}
			evs = append(evs, chunk(0, map[string]any{"tool_calls": []any{tc}}, nil))
		}
		return evs
	}
	a := frags(0, "call_a", "send", cutAt(`{"to":"`+f.email+`","n":1}`, []int{3, 9, 14, 20}))
	b := frags(1, "call_b", "send", cutAt(`{"to":"`+toolB+`"}`, []int{4, 10, 15}))
	var body strings.Builder
	for i := 0; i < max(len(a), len(b)); i++ { // parallel calls interleave
		if i < len(a) {
			body.WriteString(a[i])
		}
		if i < len(b) {
			body.WriteString(b[i])
		}
	}
	body.WriteString(chunk(0, map[string]any{}, "tool_calls") + done)
	out, _ := rewrite(t, f, body.String())
	got := argsByTool(parseOut(t, out))
	if got[[2]int{0, 0}] != `{"to":"john@acme.com","n":1}` || got[[2]int{0, 1}] != `{"to":"jane@acme.com"}` {
		t.Errorf("arguments = %v", got)
	}
	for k, v := range got {
		var x map[string]any
		if err := json.Unmarshal([]byte(v), &x); err != nil {
			t.Errorf("tool %v arguments are not valid JSON: %v: %s", k, err, v)
		}
	}
	// Ids and names ride along untouched.
	if !strings.Contains(out, `"id":"call_a"`) || !strings.Contains(out, `"name":"send"`) {
		t.Errorf("tool call header lost:\n%s", out)
	}
}

func TestHeldToolArgumentsFlushIntoFinishChunk(t *testing.T) {
	f := newFixture(t)
	tc := map[string]any{"index": 0, "id": "c", "type": "function", "function": map[string]any{"name": "f", "arguments": `{"x":"<EMAIL_7f`}}
	body := chunk(0, map[string]any{"tool_calls": []any{tc}}, nil) + chunk(0, map[string]any{}, "tool_calls") + done
	out, _ := rewrite(t, f, body)
	evs := parseOut(t, out)
	if got := argsByTool(evs)[[2]int{0, 0}]; got != `{"x":"<EMAIL_7f` {
		t.Errorf("arguments = %v", got)
	}
	last := evs[len(evs)-2].json["choices"].([]any)[0].(map[string]any)
	if last["finish_reason"] != "tool_calls" {
		t.Errorf("finish chunk is not the last data chunk:\n%s", out)
	}
}

func TestURLGuardAcrossChunks(t *testing.T) {
	f := newFixture(t)
	text := "go https://evil.test/?e=" + f.email + " then " + f.email
	for cut := 0; cut <= len(text); cut++ {
		out, st := rewrite(t, f, streamOf(0, cutAt(text, []int{cut}), "stop"))
		want := "go https://evil.test/?e=" + f.email + " then john@acme.com"
		if got := contentByChoice(parseOut(t, out))[0]; got != want {
			t.Fatalf("cut %d: got %q", cut, got)
		}
		if st.URLSkipped != 1 {
			t.Fatalf("cut %d: URLSkipped = %d", cut, st.URLSkipped)
		}
	}
}

func TestUnknownTokenStaysVisibleAndIsCounted(t *testing.T) {
	f := newFixture(t)
	out, st := rewrite(t, f, streamOf(0, []string{"mail <EMAIL_", "deadbeef> now"}, "stop"))
	if got := contentByChoice(parseOut(t, out))[0]; got != "mail <EMAIL_deadbeef> now" {
		t.Errorf("content = %q", got)
	}
	if st.Unknown != 1 {
		t.Errorf("Unknown = %d", st.Unknown)
	}
}

// A model that echoes the token with HTML-escaped brackets inside tool
// arguments is a known miss: the rewriter does not decode the escape.
func TestMissEscapedBracketsInArguments(t *testing.T) {
	f := newFixture(t)
	esc := string(rune(92)) + "u003c"
	args := `{"to":"` + esc + strings.TrimPrefix(f.email, "<")
	tc := map[string]any{"index": 0, "id": "c", "type": "function", "function": map[string]any{"name": "f", "arguments": args}}
	out, _ := rewrite(t, f, chunk(0, map[string]any{"tool_calls": []any{tc}}, nil)+chunk(0, map[string]any{}, "stop")+done)
	if got := argsByTool(parseOut(t, out))[[2]int{0, 0}]; strings.Contains(got, "john@acme.com") {
		t.Errorf("escaped form was restored; update the README miss table: %q", got)
	}
}

func TestFastPathIsByteIdentical(t *testing.T) {
	f := newFixture(t)
	body := chunk(0, map[string]any{"role": "assistant", "content": ""}, nil) +
		contentChunk(0, "Hello, world") + contentChunk(0, " and more") +
		chunk(0, map[string]any{}, "stop") +
		event(map[string]any{"id": "x", "object": "chat.completion.chunk", "choices": []any{}, "usage": map[string]any{"total_tokens": 9}}) +
		": keep-alive\n\n" + done
	out, st := rewrite(t, f, body)
	if out != body {
		t.Errorf("stream without tokens was altered:\n got  %q\n want %q", out, body)
	}
	if st.Rewritten != 0 || st.Passthrough != st.Events {
		t.Errorf("stats = %+v", st)
	}
}

func TestFramingVariants(t *testing.T) {
	f := newFixture(t)
	j := func(s string) string {
		b, _ := jsonx.Encode(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": s}, "finish_reason": nil}}})
		return string(b)
	}
	// CRLF endings, "data:" without a space, extra fields, comments.
	body := ": hello\r\n\r\n" +
		"event: message\r\nid: 7\r\ndata:" + j("a "+f.email[:6]) + "\r\n\r\n" +
		"data: " + j(f.email[6:]+" b") + "\r\n\r\n" +
		"data: [DONE]\r\n\r\n"
	out, _ := rewrite(t, f, body)
	if got := contentByChoice(parseOut(t, strings.ReplaceAll(out, "\r\n", "\n")))[0]; got != "a john@acme.com b" {
		t.Errorf("content = %q\n%q", got, out)
	}
	if !strings.Contains(out, "event: message\r\nid: 7\r\ndata: ") {
		t.Errorf("event/id lines not preserved: %q", out)
	}
	if !strings.HasPrefix(out, ": hello\r\n\r\n") || !strings.HasSuffix(out, "data: [DONE]\r\n\r\n") {
		t.Errorf("comment or DONE altered: %q", out)
	}
}

func TestNoHTMLEscapingInRewrittenEvents(t *testing.T) {
	f := newFixture(t)
	out, _ := rewrite(t, f, streamOf(0, []string{"<b>" + f.email + "</b> & co"}, "stop"))
	if strings.Contains(out, string(rune(92))+"u003c") || strings.Contains(out, string(rune(92))+"u0026") {
		t.Errorf("rewritten event is HTML-escaped: %s", out)
	}
	if got := contentByChoice(parseOut(t, out))[0]; got != "<b>john@acme.com</b> & co" {
		t.Errorf("content = %q", got)
	}
}

func TestEventSizeLimit(t *testing.T) {
	f := newFixture(t)
	big := contentChunk(0, strings.Repeat("x", 4096))
	rw := New(ctx, io.NopCloser(strings.NewReader(big)), f.sess, Options{MaxEventBytes: 1024})
	if _, err := io.ReadAll(rw); err != ErrEventTooLarge {
		t.Errorf("err = %v, want ErrEventTooLarge", err)
	}
}

// The rewriter must forward each event as it arrives, not when the stream ends.
func TestStreamsIncrementally(t *testing.T) {
	f := newFixture(t)
	pr, pw := io.Pipe()
	rw := New(ctx, pr, f.sess, Options{})
	go func() {
		pw.Write([]byte(contentChunk(0, "first ")))
		time.Sleep(300 * time.Millisecond)
		pw.Write([]byte(contentChunk(0, "second")))
		pw.Close()
	}()
	start := time.Now()
	buf := make([]byte, 4096)
	n, err := rw.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Errorf("first event took %v: the rewriter buffered", elapsed)
	}
	if !strings.Contains(string(buf[:n]), "first ") {
		t.Errorf("first read = %q", buf[:n])
	}
	rest, _ := io.ReadAll(rw)
	if !strings.Contains(string(rest), "second") {
		t.Errorf("rest = %q", rest)
	}
}

func TestAllowURLs(t *testing.T) {
	f := newFixture(t)
	rw := New(ctx, io.NopCloser(strings.NewReader(streamOf(0, []string{"https://x.test/?e=" + f.email}, "stop"))), f.sess, Options{AllowURLs: true})
	out, _ := io.ReadAll(rw)
	if !strings.Contains(string(out), "https://x.test/?e=john@acme.com") {
		t.Errorf("AllowURLs ignored: %s", out)
	}
}

func BenchmarkRewrite(b *testing.B) {
	f := newFixture(b)
	var parts []string
	for i := 0; i < 200; i++ {
		parts = append(parts, fmt.Sprintf("word%d ", i))
	}
	parts[100] = "mail " + f.email + " "
	body := streamOf(0, parts, "stop")
	b.SetBytes(int64(len(body)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rw := New(ctx, io.NopCloser(bytes.NewReader([]byte(body))), f.sess, Options{})
		io.Copy(io.Discard, rw)
	}
}
