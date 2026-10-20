package eval

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/mask"
	"github.com/vikasgowda3007/pseudonymbench/internal/proxy"
	"github.com/vikasgowda3007/pseudonymbench/internal/synth"
	"github.com/vikasgowda3007/pseudonymbench/internal/vault"
)

func TestRegions(t *testing.T) {
	tests := []struct {
		name, orig, masked string
		want               []string // substrings of orig, in order
		kinds              [][]detect.Kind
		replaced           []int
	}{
		{"identical", "nothing to see", "nothing to see", nil, nil, nil},
		{"email", "Email john@acme.com about it.", "Email <EMAIL_0123abcd> about it.",
			[]string{"john@acme.com"}, [][]detect.Kind{{detect.Email}}, []int{1}},
		{"trailing punctuation", "mail john@acme.com.", "mail <EMAIL_0123abcd>.",
			[]string{"john@acme.com"}, [][]detect.Kind{{detect.Email}}, []int{1}},
		{"leading paren", "call (415) 555-2671 now", "call <PHONE_0123abcd> now",
			[]string{"(415) 555-2671"}, [][]detect.Kind{{detect.Phone}}, []int{1}},
		{"card with spaces", "card 4111 1111 1111 1111 ok", "card <CARD_0123abcd> ok",
			[]string{"4111 1111 1111 1111"}, [][]detect.Kind{{detect.Card}}, []int{1}},
		{"two adjacent", "a@b.co 415-555-2671 end", "<EMAIL_00000001> <PHONE_00000002> end",
			[]string{"a@b.co 415-555-2671"}, [][]detect.Kind{{detect.Email, detect.Phone}}, []int{2}},
		{"two apart", "a@b.co and 078-05-1120", "<EMAIL_00000001> and <SSN_00000002>",
			[]string{"a@b.co", "078-05-1120"}, [][]detect.Kind{{detect.Email}, {detect.SSN}}, []int{1, 1}},
		{"other tools' placeholders", "to x@y.com or 415-555-2671", "to [EMAIL_ADDRESS] or <PHONE_NUMBER>",
			[]string{"x@y.com", "415-555-2671"}, [][]detect.Kind{{detect.Email}, {detect.Phone}}, []int{1, 1}},
		{"numbered placeholder", "ssn 078-05-1120", "ssn {{US_SSN_1}}",
			[]string{"078-05-1120"}, [][]detect.Kind{{detect.SSN}}, []int{1}},
		{"deletion", "keep secret this", "keep this", []string{"secret"}, [][]detect.Kind{nil}, []int{1}},
		{"unicode", "naïve ünïcode john@acme.com ok", "naïve ünïcode <EMAIL_0123abcd> ok",
			[]string{"john@acme.com"}, [][]detect.Kind{{detect.Email}}, []int{1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Regions(tc.orig, tc.masked)
			if len(got) != len(tc.want) {
				t.Fatalf("regions = %+v, want %d", got, len(tc.want))
			}
			for i, r := range got {
				if span := tc.orig[r.Start:r.End]; span != tc.want[i] {
					t.Errorf("region %d = %q, want %q", i, span, tc.want[i])
				}
				if r.Replaced != tc.replaced[i] {
					t.Errorf("region %d replaced %d, want %d", i, r.Replaced, tc.replaced[i])
				}
				if len(r.Kinds) != len(tc.kinds[i]) {
					t.Errorf("region %d kinds = %v, want %v", i, r.Kinds, tc.kinds[i])
					continue
				}
				for k := range r.Kinds {
					if r.Kinds[k] != tc.kinds[i][k] {
						t.Errorf("region %d kinds = %v, want %v", i, r.Kinds, tc.kinds[i])
					}
				}
			}
		})
	}
}

func TestScoreCounts(t *testing.T) {
	samples := []synth.Sample{
		{Text: "mail a@b.co and tel 415-555-2671", Spans: []synth.Span{
			{Kind: detect.Email, Start: 5, End: 11}, {Kind: detect.Phone, Start: 20, End: 32}}},
		{Text: "clean text here"},
		{Text: "ssn 078-05-1120", Spans: []synth.Span{{Kind: detect.SSN, Start: 4, End: 15}}},
	}
	preds := [][]Region{
		{{Start: 5, End: 11, Kinds: []detect.Kind{detect.Email}, Replaced: 1}}, // phone missed
		{{Start: 0, End: 5, Kinds: []detect.Kind{detect.Phone}, Replaced: 1}},  // false positive on clean text
		{{Start: 2, End: 12, Kinds: []detect.Kind{detect.SSN}, Replaced: 1}},   // overlaps, not exact
	}
	rep := Score("x", samples, preds, nil)
	e, p, s := rep.Kinds[detect.Email], rep.Kinds[detect.Phone], rep.Kinds[detect.SSN]
	if e.LeakRate != 0 || p.LeakRate != 1 || s.LeakRate != 1-8.0/11 {
		t.Errorf("leak rates: email %v, phone %v, ssn %v", e.LeakRate, p.LeakRate, s.LeakRate)
	}
	if e.RecallOver != 1 || e.RecallExact != 1 || e.PrecisionTyp != 1 {
		t.Errorf("email = %+v", e)
	}
	if p.Gold != 1 || p.HitOverlap != 0 || p.Predicted != 1 || p.PredictedTP != 0 {
		t.Errorf("phone = %+v", p)
	}
	if s.RecallOver != 1 || s.RecallExact != 0 || s.PrecisionTyp != 1 {
		t.Errorf("ssn = %+v", s)
	}
	if rep.Regions != 3 || rep.RegionsTP != 2 || rep.CleanSamples != 1 || rep.CleanWithRegion != 1 || rep.CleanFPRate != 1 {
		t.Errorf("overall = %+v", rep)
	}
	if !strings.Contains(rep.Markdown(), "| EMAIL |") {
		t.Errorf("markdown lacks a row:\n%s", rep.Markdown())
	}
}

func TestScoreSkipsErrors(t *testing.T) {
	samples := []synth.Sample{{Text: "a"}, {Text: "b"}}
	rep := Score("x", samples, make([][]Region, 2), []bool{true, false})
	if rep.Errors != 1 || rep.Samples != 1 {
		t.Errorf("report = %+v", rep)
	}
}

func TestJSONLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.jsonl")
	in := synth.New(5).Dataset(20)
	if err := SaveJSONL(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadJSONL(path)
	if err != nil || len(out) != len(in) {
		t.Fatalf("load: %v, %d samples", err, len(out))
	}
	for i := range in {
		if in[i].Text != out[i].Text || len(in[i].Spans) != len(out[i].Spans) {
			t.Fatalf("sample %d differs", i)
		}
	}
}

// --- end to end through the real proxy and a recording upstream -------------

type recorder struct {
	*httptest.Server
	mu   sync.Mutex
	seen map[string]string
}

func newRecorder(t *testing.T) *recorder {
	t.Helper()
	r := &recorder{seen: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.seen[req.Header.Get("X-Eval-Id")] = string(b)
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	})
	mux.HandleFunc("/_received/", func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		b, ok := r.seen[strings.TrimPrefix(req.URL.Path, "/_received/")]
		r.mu.Unlock()
		if !ok {
			http.NotFound(w, req)
			return
		}
		io.WriteString(w, b)
	})
	r.Server = httptest.NewServer(mux)
	t.Cleanup(r.Close)
	return r
}

func proxyFor(t *testing.T, up *recorder, pol mask.Policy) *httptest.Server {
	t.Helper()
	u, _ := url.Parse(up.URL)
	h, err := proxy.New(proxy.Config{Upstream: u, Masker: mask.New(pol), Vault: vault.NewMemory(vault.MemoryOptions{}), SessionKey: []byte("k")})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

// The proxy-path measurement must agree with running the detector directly:
// same regions, found by diffing instead of by looking inside.
func TestProxyPathMatchesDetector(t *testing.T) {
	up := newRecorder(t)
	px := proxyFor(t, up, nil)
	samples := synth.New(11).Dataset(400)

	preds, errs := RunProxy(context.Background(),
		Target{URL: px.URL + "/v1/chat/completions", MockURL: up.URL, Concurrency: 8}, "t", samples)
	viaProxy := Score("proxy", samples, preds, errs)
	direct := Score("detector", samples, DetectorRegions(samples), nil)

	if viaProxy.Errors != 0 {
		t.Fatalf("%d samples errored", viaProxy.Errors)
	}
	for _, k := range detect.Kinds {
		a, b := viaProxy.Kinds[k], direct.Kinds[k]
		if a.Gold != b.Gold || a.HitOverlap != b.HitOverlap || a.Predicted != b.Predicted || a.PredictedTP != b.PredictedTP {
			t.Errorf("%s: proxy %+v != detector %+v", k, a, b)
		}
		if a.RecallOver < 0.99 {
			t.Errorf("%s recall %.3f on synthetic data", k, a.RecallOver)
		}
	}
	if viaProxy.Regions != direct.Regions || viaProxy.CleanWithRegion != direct.CleanWithRegion {
		t.Errorf("regions: proxy %d/%d clean-FP, detector %d/%d", viaProxy.Regions, viaProxy.CleanWithRegion, direct.Regions, direct.CleanWithRegion)
	}
	t.Logf("\n%s", viaProxy.Markdown())
}

func TestFieldMatrix(t *testing.T) {
	up := newRecorder(t)
	px := proxyFor(t, up, nil)
	ctx := context.Background()

	masked := RunFields(ctx, Target{URL: px.URL + "/v1/chat/completions", MockURL: up.URL}, "m", "pseudonymbench")
	for _, r := range masked.Results {
		if r.Outcome != Masked {
			t.Errorf("field %q: %s (status %d)", r.Field, r.Outcome, r.Status)
		}
	}
	// Control: skipping the proxy must show every field leaking, otherwise the
	// harness could not tell a masker from a no-op.
	control := RunFields(ctx, Target{URL: up.URL + "/v1/chat/completions", MockURL: up.URL}, "c", "no proxy")
	for _, r := range control.Results {
		if r.Outcome != Leaked {
			t.Errorf("control field %q: %s", r.Field, r.Outcome)
		}
	}
	md := FieldsMarkdown([]FieldReport{masked, control})
	if !strings.Contains(md, "| no proxy |") || !strings.Contains(md, "no proxy 21/21") {
		t.Errorf("matrix markdown:\n%s", md)
	}
}

func TestFieldMatrixReportsRejection(t *testing.T) {
	up := newRecorder(t)
	px := proxyFor(t, up, mask.Policy{detect.Email: mask.ActionBlock})
	rep := RunFields(context.Background(), Target{URL: px.URL + "/v1/chat/completions", MockURL: up.URL}, "b", "blocking")
	for _, r := range rep.Results {
		if r.Outcome != Rejected {
			t.Errorf("field %q: %s, want rejected", r.Field, r.Outcome)
		}
	}
}

func TestPresidioRegions(t *testing.T) {
	// A fake analyzer: reports an email (character offsets, after multi-byte
	// text), the same span again under another type, and a person.
	text := "Zoë wrote to a@b.co about Anna"
	start := len([]rune("Zoë wrote to "))
	analyzer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analyze" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"language":"en"`) || !strings.Contains(string(body), `"score_threshold":0.4`) {
			t.Errorf("request body = %s", body)
		}
		fmt.Fprintf(w, `[{"entity_type":"EMAIL_ADDRESS","start":%d,"end":%d,"score":1.0},`+
			`{"entity_type":"URL","start":%d,"end":%d,"score":0.5},`+
			`{"entity_type":"PERSON","start":%d,"end":%d,"score":0.85},`+
			`{"entity_type":"DATE_TIME","start":0,"end":3,"score":0.2}]`,
			start, start+6, start, start+6, len([]rune(text))-4, len([]rune(text)))
	}))
	defer analyzer.Close()

	samples := []synth.Sample{{Text: text}}
	preds, errs := PresidioRegions(context.Background(), analyzer.URL, 0.4, 2, samples)
	if errs[0] || len(preds[0]) != 2 {
		t.Fatalf("errs %v preds %+v", errs, preds)
	}
	em := preds[0][0]
	if text[em.Start:em.End] != "a@b.co" || len(em.Kinds) != 1 || em.Kinds[0] != detect.Email {
		t.Errorf("email region = %q %v", text[em.Start:em.End], em.Kinds)
	}
	if p := preds[0][1]; text[p.Start:p.End] != "Anna" || len(p.Kinds) != 0 {
		t.Errorf("person region = %q %v", text[p.Start:p.End], p.Kinds)
	}
}

func TestPresidioRegionsReportsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer srv.Close()
	_, errs := PresidioRegions(context.Background(), srv.URL, 0, 1, []synth.Sample{{Text: "x"}})
	if !errs[0] {
		t.Error("a 500 from the analyzer should count as an error")
	}
}

func TestConfusionMatrix(t *testing.T) {
	samples := []synth.Sample{
		{Text: "a@b.co 415-555-2671 078-05-1120 4111", Spans: []synth.Span{
			{Kind: detect.Email, Start: 0, End: 6},   // called EMAIL
			{Kind: detect.Phone, Start: 7, End: 19},  // called SSN: a type confusion
			{Kind: detect.SSN, Start: 20, End: 31},   // changed, placeholder not recognised
			{Kind: detect.Card, Start: 32, End: 36}}, // missed
		},
	}
	preds := [][]Region{{
		{Start: 0, End: 6, Kinds: []detect.Kind{detect.Email}, Replaced: 1},
		{Start: 7, End: 19, Kinds: []detect.Kind{detect.SSN}, Replaced: 1},
		{Start: 20, End: 31, Replaced: 1},
	}}
	rep := Score("x", samples, preds, nil)
	c := rep.Confusion
	if c[detect.Email]["EMAIL"] != 1 || c[detect.Phone]["SSN"] != 1 || c[detect.SSN]["UNTYPED"] != 1 || c[detect.Card]["MISSED"] != 1 {
		t.Errorf("confusion = %v", c)
	}
	md := rep.Markdown()
	if !strings.Contains(md, "gold \\ called") || !strings.Contains(md, "| PHONE |") {
		t.Errorf("markdown lacks the matrix:\n%s", md)
	}
	// A report with no confusion prints no matrix.
	clean := Score("x", samples[:0], nil, nil)
	if strings.Contains(clean.Markdown(), "gold \\ called") {
		t.Error("matrix printed with nothing to show")
	}
}
