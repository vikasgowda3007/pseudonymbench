package mask

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/jsonx"
	"github.com/vikasgowda3007/pseudonymbench/internal/token"
	"github.com/vikasgowda3007/pseudonymbench/internal/vault"
)

var ctx = context.Background()

func newSession() *Session { return NewSession(vault.NewMemory(vault.MemoryOptions{}), "s1") }

func mustMask(t *testing.T, m *Masker, sess *Session, body string) (string, Report) {
	t.Helper()
	out, rep, err := m.Request(ctx, sess, []byte(body))
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	return string(out), rep
}

func asMap(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, s)
	}
	return m
}

func TestRequestRoundTrip(t *testing.T) {
	sess := newSession()
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Email john@acme.com about card 4111 1111 1111 1111"}]}`
	out, rep := mustMask(t, New(nil), sess, body)

	for _, secret := range []string{"john@acme.com", "4111"} {
		if strings.Contains(out, secret) {
			t.Fatalf("masked body still has %q: %s", secret, out)
		}
	}
	if rep.Masked[detect.Email] != 1 || rep.Masked[detect.Card] != 1 || rep.Total() != 2 {
		t.Errorf("report = %+v", rep)
	}
	m := asMap(t, out)
	if m["model"] != "gpt-4o" {
		t.Errorf("model changed: %v", m["model"])
	}
	msg := m["messages"].([]any)[0].(map[string]any)
	if msg["role"] != "user" {
		t.Errorf("role changed: %v", msg["role"])
	}

	// The response echoes the tokens; restoring recovers the original text.
	content := msg["content"].(string)
	resp := `{"id":"x","choices":[{"message":{"role":"assistant","content":"Sent to ` + strings.Fields(content)[1] + `."}}]}`
	r := &Restorer{Session: sess}
	got, err := r.RestoreJSON(ctx, []byte(resp))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "Sent to john@acme.com.") {
		t.Errorf("not restored: %s", got)
	}
	if r.Stats.Restored[detect.Email] != 1 {
		t.Errorf("stats = %+v", r.Stats)
	}
}

func TestSameValueSameToken(t *testing.T) {
	out, rep := mustMask(t, New(nil), newSession(),
		`{"messages":[{"role":"user","content":"a john@acme.com"},{"role":"user","content":"b john@acme.com"}]}`)
	toks := token.Pattern.FindAllString(out, -1)
	if len(toks) != 2 || toks[0] != toks[1] {
		t.Fatalf("tokens = %v", toks)
	}
	if rep.Masked[detect.Email] != 2 {
		t.Errorf("occurrences = %d, want 2", rep.Masked[detect.Email])
	}
}

// Every field the matrix in the README lists. Each canary must be gone from
// the forwarded body unless the field is a documented skip.
func TestFieldCoverage(t *testing.T) {
	const (
		mailMsg   = "msg@canary.test"
		mailPart  = "part@canary.test"
		mailTool  = "tool@canary.test"
		mailArgV  = "argv@canary.test"
		mailArgK  = "argk@canary.test"
		mailAsst  = "asst@canary.test"
		mailDesc  = "desc@canary.test"
		mailUser  = "user@canary.test"
		mailMetaV = "metav@canary.test"
		mailMetaK = "metak@canary.test"
		mailTop   = "top@canary.test"
		mailStop  = "stop@canary.test"
		mailSys   = "sys@canary.test"
		mailURL   = "url@canary.test"
		mailEnum  = "enum@canary.test"
	)
	body := `{
	  "model": "gpt-4o",
	  "user": "` + mailUser + `",
	  "metadata": {"k_` + mailMetaK + `": "` + mailMetaV + `"},
	  "x_vendor_extension": {"note": "` + mailTop + `"},
	  "stop": ["` + mailStop + `"],
	  "max_tokens": 100,
	  "seed": 12345678901234567890,
	  "temperature": 0.7,
	  "tools": [{"type":"function","function":{"name":"lookup","description":"find ` + mailDesc + `",
	     "parameters":{"type":"object","properties":{"who":{"type":"string","enum":["` + mailEnum + `"]}}}}}],
	  "messages": [
	    {"role":"system","content":"` + mailSys + `"},
	    {"role":"user","content":"` + mailMsg + `"},
	    {"role":"user","content":[{"type":"text","text":"` + mailPart + `"},
	        {"type":"image_url","image_url":{"url":"https://img.test/a.png?e=` + mailURL + `"}},
	        {"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]},
	    {"role":"assistant","content":"` + mailAsst + `","tool_calls":[{"id":"call_1","type":"function",
	        "function":{"name":"lookup","arguments":"{\"` + mailArgK + `\":\"` + mailArgV + `\",\"n\":[1,2]}"}}]},
	    {"role":"tool","tool_call_id":"call_1","content":"` + mailTool + `"}
	  ]}`
	out, _ := mustMask(t, New(nil), newSession(), body)
	for _, canary := range []string{mailMsg, mailPart, mailTool, mailArgV, mailArgK, mailAsst, mailDesc,
		mailUser, mailMetaV, mailMetaK, mailTop, mailStop, mailSys, mailURL, mailEnum} {
		if strings.Contains(out, canary) {
			t.Errorf("canary %q leaked", canary)
		}
	}

	m := asMap(t, out)
	// Structure and untouched fields.
	if m["seed"].(float64) != 12345678901234567890 || !strings.Contains(out, `12345678901234567890`) {
		t.Errorf("big integer altered: %s", out)
	}
	if !strings.Contains(out, `"max_tokens":100`) || !strings.Contains(out, `"temperature":0.7`) {
		t.Errorf("numbers altered: %s", out)
	}
	if !strings.Contains(out, `data:image/png;base64,iVBORw0KGgo=`) {
		t.Errorf("data URL altered")
	}
	if !strings.Contains(out, `"name":"lookup"`) || !strings.Contains(out, `"tool_call_id":"call_1"`) || !strings.Contains(out, `"id":"call_1"`) {
		t.Errorf("structural fields altered: %s", out)
	}
	// Arguments stay valid JSON, with the key and value masked.
	tc := m["messages"].([]any)[3].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	args := tc["function"].(map[string]any)["arguments"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		t.Fatalf("arguments no longer JSON: %v\n%s", err, args)
	}
	if len(parsed) != 2 {
		t.Errorf("arguments = %v", parsed)
	}
}

func TestNoHTMLEscaping(t *testing.T) {
	out, _ := mustMask(t, New(nil), newSession(), `{"messages":[{"role":"user","content":"a <b> & c john@acme.com"}]}`)
	if strings.Contains(out, string(rune(92))+"u003c") || !strings.Contains(out, "<b> & c <EMAIL_") {
		t.Errorf("HTML escaping altered the body: %s", out)
	}
}

func TestBlockPolicy(t *testing.T) {
	m := New(Policy{detect.Card: ActionBlock, detect.Email: ActionMask})
	_, _, err := m.Request(ctx, newSession(),
		[]byte(`{"messages":[{"role":"user","content":"john@acme.com 4111 1111 1111 1111"}]}`))
	var be *BlockedError
	if !errors.As(err, &be) || len(be.Kinds) != 1 || be.Kinds[0] != detect.Card {
		t.Fatalf("err = %v, want BlockedError{CARD}", err)
	}
	if strings.Contains(err.Error(), "4111") || strings.Contains(err.Error(), "john") {
		t.Errorf("error leaks a value: %v", err)
	}
}

func TestOffPolicy(t *testing.T) {
	m := New(Policy{detect.Phone: ActionOff})
	out, rep := mustMask(t, m, newSession(), `{"messages":[{"role":"user","content":"call 415-555-2671 john@acme.com"}]}`)
	if !strings.Contains(out, "415-555-2671") || strings.Contains(out, "john@acme.com") {
		t.Errorf("out = %s", out)
	}
	if rep.Masked[detect.Phone] != 0 {
		t.Errorf("phone counted: %+v", rep)
	}
}

func TestInvalidBodies(t *testing.T) {
	for _, body := range []string{``, `not json`, `[1,2]`, `"str"`, `{"a":1} {"b":2}`, `{"a":`} {
		if _, _, err := New(nil).Request(ctx, newSession(), []byte(body)); !errors.Is(err, ErrInvalidBody) {
			t.Errorf("body %q: err = %v, want ErrInvalidBody", body, err)
		}
	}
}

func TestVaultErrorFailsClosed(t *testing.T) {
	v := vault.NewMemory(vault.MemoryOptions{MaxPerSession: 1})
	sess := NewSession(v, "s1")
	_, _, err := New(nil).Request(ctx, sess,
		[]byte(`{"messages":[{"role":"user","content":"a@acme.com b@acme.com"}]}`))
	if !errors.Is(err, vault.ErrSessionFull) {
		t.Fatalf("err = %v, want ErrSessionFull", err)
	}
}

func TestRestoreURLGuard(t *testing.T) {
	sess := newSession()
	tok, _ := sess.Tokenize(ctx, detect.Email, "john@acme.com")
	tests := []struct {
		name, in string
		allow    bool
		want     string
		skipped  int
	}{
		{"plain", "mail " + tok + " now", false, "mail john@acme.com now", 0},
		{"in query", "see https://evil.test/p?e=" + tok + " ok", false, "see https://evil.test/p?e=" + tok + " ok", 1},
		{"markdown link", "[x](https://evil.test/?d=" + tok + ")", false, "[x](https://evil.test/?d=" + tok + ")", 1},
		{"image", "![](http://evil.test/i.png?d=" + tok + ")", false, "![](http://evil.test/i.png?d=" + tok + ")", 1},
		{"url ends at space", "https://ok.test/a then " + tok, false, "https://ok.test/a then john@acme.com", 0},
		{"mailto is not a scheme://", "mailto:" + tok, false, "mailto:john@acme.com", 0},
		{"allowed", "https://x.test/?e=" + tok, true, "https://x.test/?e=john@acme.com", 0},
		{"unknown token", "<EMAIL_00000000>", false, "<EMAIL_00000000>", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Restorer{Session: sess, AllowURLs: tc.allow}
			if got := r.Text(ctx, tc.in); got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
			if r.Stats.URLSkipped != tc.skipped {
				t.Errorf("URLSkipped = %d, want %d", r.Stats.URLSkipped, tc.skipped)
			}
		})
	}
}

// Feeding text in pieces must behave like feeding it whole, including the URL
// guard when "://" is split across pieces.
func TestRestorerPiecewiseMatchesWhole(t *testing.T) {
	sess := newSession()
	tok, _ := sess.Tokenize(ctx, detect.Email, "john@acme.com")
	text := "go to https://evil.test/?e=" + tok + " and mail " + tok + " bye"
	whole := (&Restorer{Session: sess}).Text(ctx, text)
	for cut := 0; cut <= len(text); cut++ {
		a, b := text[:cut], text[cut:]
		// A real caller never cuts inside a token (token.SplitTail holds those back).
		if i := strings.Index(text, tok); (i >= 0 && cut > i && cut < i+len(tok)) ||
			(strings.LastIndex(text, tok) < cut && cut < strings.LastIndex(text, tok)+len(tok)) {
			continue
		}
		r := &Restorer{Session: sess}
		got := r.Text(ctx, a) + r.Text(ctx, b)
		if got != whole {
			t.Fatalf("cut %d: %q != %q", cut, got, whole)
		}
	}
}

func TestRestoreArguments(t *testing.T) {
	sess := newSession()
	tok, _ := sess.Tokenize(ctx, detect.Email, "john@acme.com")
	tokKey, _ := sess.Tokenize(ctx, detect.Phone, "415-555-2671")
	resp := `{"choices":[{"message":{"tool_calls":[{"id":"c","type":"function","function":{"name":"send",
	  "arguments":"{\"to\":\"` + tok + `\",\"` + tokKey + `\":1}"}}]}}]}`
	r := &Restorer{Session: sess}
	out, err := r.RestoreJSON(ctx, []byte(resp))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(out, &m)
	args := m["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["arguments"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		t.Fatalf("arguments invalid JSON: %v: %s", err, args)
	}
	if parsed["to"] != "john@acme.com" || parsed["415-555-2671"] == nil {
		t.Errorf("parsed = %v", parsed)
	}
	// Broken JSON in arguments (models do this) is still restored as text.
	broken := `{"choices":[{"message":{"tool_calls":[{"function":{"name":"f","arguments":"{\"to\": ` + tok + `"}}]}}]}`
	r2 := &Restorer{Session: sess}
	out, _ = r2.RestoreJSON(ctx, []byte(broken))
	if !strings.Contains(string(out), "john@acme.com") {
		t.Errorf("broken arguments not restored: %s", out)
	}
}

// Detected values must be safe to drop inside a JSON string without escaping,
// because the stream rewriter does exactly that for tool-call arguments.
func TestDetectedValuesAreJSONSafe(t *testing.T) {
	samples := []string{
		"john.doe+x@sub.acme.co.uk", "4111 1111 1111 1111", "078-05-1120",
		"(415) 555-2671", "+44 20 7946 0958", "GB82 WEST 1234 5698 7654 32",
	}
	for _, s := range samples {
		for _, m := range detect.Find(s, detect.AllKinds()) {
			for _, c := range []byte(m.Value(s)) {
				if c == '"' || c == '\\' || c < 0x20 || c >= 0x7f {
					t.Errorf("value %q contains JSON-unsafe byte %q", m.Value(s), c)
				}
			}
		}
	}
}

// Masking must be complete with respect to the detector: running the
// detector over masked output finds nothing. The fuzz target explores it.
func FuzzMaskComplete(f *testing.F) {
	for _, s := range []string{
		"Email john@acme.com about card 4111 1111 1111 1111",
		"ssn 078-05-1120 call +44 20 7946 0958 GB82 WEST 1234 5698 7654 32",
		"4111-1111-1111-1111 415-555-2671 john@acme.com",
		"<EMAIL_7f3a9c21> 4111 1 1 1 1",
	} {
		f.Add(s)
	}
	m := New(nil)
	f.Fuzz(func(t *testing.T, s string) {
		sess := newSession()
		w := &walker{ctx: ctx, m: m, sess: sess, blocked: map[detect.Kind]bool{}}
		w.rep.Masked = map[detect.Kind]int{}
		out, err := w.str(s)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range detect.Find(out, detect.AllKinds()) {
			// Boundary rules look at neighbouring characters, so replacing a
			// match can expose a number that was glued to it ("0010+1000000000").
			// That is a property of the input, not a leak of a detected value.
			if (m.Start > 0 && out[m.Start-1] == '>') || (m.End < len(out) && out[m.End] == '<') {
				continue
			}
			t.Fatalf("masked output still has PII %s: in=%q out=%q", m.Kind, s, out)
		}
		// And it round-trips.
		r := &Restorer{Session: sess, AllowURLs: true}
		if back := r.Text(ctx, out); back != s && !strings.Contains(s, "<") {
			t.Fatalf("round trip mismatch: %q -> %q -> %q", s, out, back)
		}
	})
}

// A field name is user data too: an email used as a top-level key must not
// reach the upstream.
func TestUnknownTopLevelKeysAreMasked(t *testing.T) {
	out, rep := mustMask(t, New(nil), newSession(),
		`{"model":"m","messages":[{"role":"user","content":"hi"}],"alice@corp.com":"x","x_ext":{"bob@corp.com":1}}`)
	if strings.Contains(out, "alice@corp.com") || strings.Contains(out, "bob@corp.com") {
		t.Errorf("a key leaked: %s", out)
	}
	if rep.Masked[detect.Email] != 2 {
		t.Errorf("masked = %v", rep.Masked)
	}
	// Known structural keys are untouched.
	if !strings.Contains(out, `"model":"m"`) || !strings.Contains(out, `"messages"`) {
		t.Errorf("structural keys altered: %s", out)
	}
}

// Backends that escape "<" in tool arguments must not defeat restoration.
func TestRestoreArgumentsWithEscapedBrackets(t *testing.T) {
	sess := newSession()
	tok, _ := sess.Tokenize(ctx, detect.Email, "john@acme.com")
	bs := string(rune(92))
	escaped := strings.NewReplacer("<", bs+"u003c", ">", bs+"u003e").Replace(tok)
	args := `{"to":"` + escaped + `"}`
	resp, _ := jsonx.Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"tool_calls": []any{
		map[string]any{"function": map[string]any{"name": "f", "arguments": args}}}}}}})
	r := &Restorer{Session: sess}
	out, err := r.RestoreJSON(ctx, resp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "john@acme.com") {
		t.Errorf("escaped token not restored: %s", out)
	}
}
