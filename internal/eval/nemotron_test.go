package eval

import (
	"strconv"
	"testing"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/synth"
)

func TestParsePyLiteral(t *testing.T) {
	v, err := parsePyLiteral(`[{'start': 0, 'end': 5, 'text': 'O\'Brien', 'label': "it's", 'x': None, 'ok': True, 'f': -1.5, 'u': 'caf\xe9 ü'}, ]`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.([]any)[0].(map[string]any)
	if m["text"] != "O'Brien" || m["label"] != "it's" || m["x"] != nil || m["ok"] != true || m["f"] != -1.5 || m["u"] != "café ü" {
		t.Errorf("parsed = %#v", m)
	}
	if n, _ := m["end"].(int64); n != 5 {
		t.Errorf("end = %#v", m["end"])
	}
	for _, bad := range []string{`[`, `{'a' 1}`, `'abc`, `[1] x`, `{1: 2}`} {
		if _, err := parsePyLiteral(bad); err == nil {
			t.Errorf("parsePyLiteral(%q) accepted", bad)
		}
	}
	if v, err := parsePyLiteral(`[]`); err != nil || len(v.([]any)) != 0 {
		t.Errorf("empty list: %v, %v", v, err)
	}
}

// The first test row is quoted from nvidia/Nemotron-PII (CC-BY-4.0).
func TestFromNemotron(t *testing.T) {
	text := "This Credit Card Authorization Form authorizes the transaction for the user name: lkernan. " +
		"The account number: 87425693 will be charged the specified amount using the credit debit card: 3742 912345 67890."
	spans := `[{'start': 82, 'end': 89, 'text': 'lkernan', 'label': 'user_name'}, ` +
		`{'start': 111, 'end': 119, 'text': '87425693', 'label': 'account_number'}, ` +
		`{'start': 186, 'end': 203, 'text': '3742 912345 67890', 'label': 'credit_debit_card'}]`
	s, dropped, err := FromNemotron(text, spans)
	if err != nil || dropped != 0 {
		t.Fatalf("err %v, dropped %d", err, dropped)
	}
	if len(s.Spans) != 3 || s.Spans[0].Kind != KindOther || s.Spans[1].Kind != KindOther || s.Spans[2].Kind != detect.Card {
		t.Fatalf("spans = %+v", s.Spans)
	}
	if got := s.Text[s.Spans[2].Start:s.Spans[2].End]; got != "3742 912345 67890" {
		t.Errorf("card span = %q", got)
	}
}

func TestFromNemotronConvertsCharOffsetsToBytes(t *testing.T) {
	text := "Zoë ünï wrote to a@b.co today" // multi-byte characters before the span
	start := len([]rune("Zoë ünï wrote to "))
	spans := `[{'start': ` + itoa(start) + `, 'end': ` + itoa(start+6) + `, 'text': 'a@b.co', 'label': 'email'}]`
	s, dropped, err := FromNemotron(text, spans)
	if err != nil || dropped != 0 || len(s.Spans) != 1 {
		t.Fatalf("err %v dropped %d spans %+v", err, dropped, s.Spans)
	}
	if got := s.Text[s.Spans[0].Start:s.Spans[0].End]; got != "a@b.co" || s.Spans[0].Kind != detect.Email {
		t.Errorf("span = %q (%s)", got, s.Spans[0].Kind)
	}
}

func TestFromNemotronDropsInconsistentSpans(t *testing.T) {
	s, dropped, err := FromNemotron("hello world", `[{'start': 0, 'end': 5, 'text': 'WRONG', 'label': 'email'}, {'start': 6, 'end': 99, 'text': 'x', 'label': 'email'}]`)
	if err != nil || dropped != 2 || len(s.Spans) != 0 {
		t.Errorf("err %v dropped %d spans %+v", err, dropped, s.Spans)
	}
	if _, _, err := FromNemotron("x", `not a list`); err == nil {
		t.Error("garbage accepted")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestScoreIgnoresOutOfScopeSpans(t *testing.T) {
	samples := []synth.Sample{
		// "Smith" is out-of-scope PII; masking it must not hurt precision, and
		// leaving it must not hurt recall.
		{Text: "Smith a@b.co", Spans: []synth.Span{{Kind: KindOther, Start: 0, End: 5}, {Kind: detect.Email, Start: 6, End: 12}}},
		{Text: "acct 1234 here", Spans: []synth.Span{{Kind: KindOther, Start: 5, End: 9}}},
	}
	preds := [][]Region{
		{{Start: 0, End: 5, Replaced: 1}, {Start: 6, End: 12, Kinds: []detect.Kind{detect.Email}, Replaced: 1}},
		{{Start: 5, End: 9, Kinds: []detect.Kind{detect.Phone}, Replaced: 1}},
	}
	rep := Score("x", samples, preds, nil)
	if rep.Kinds[detect.Email].RecallOver != 1 || rep.Regions != 1 || rep.RegionsTP != 1 || rep.Neutral != 2 {
		t.Errorf("report = %+v", rep)
	}
	if rep.Kinds[detect.Phone].Predicted != 0 {
		t.Errorf("a neutral region was scored as a phone prediction: %+v", rep.Kinds[detect.Phone])
	}
	if rep.CleanSamples != 0 {
		t.Errorf("a sample with only out-of-scope PII is not clean: %+v", rep)
	}
}
