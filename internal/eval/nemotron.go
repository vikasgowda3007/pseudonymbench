package eval

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/synth"
)

// KindOther marks gold spans of PII that the regex tier is not built to find
// (names, addresses, dates of birth, account numbers ...). A masker that
// changes text overlapping only such a span is not penalised, and the span
// does not count against recall: it is out of scope, not a miss.
const KindOther detect.Kind = "OTHER"

// nemotronKinds maps Nemotron-PII labels to our kinds. fax_number is phone
// shaped but is a different label; it is left out of scope on purpose so a
// phone detector is neither rewarded nor punished for it.
var nemotronKinds = map[string]detect.Kind{
	"email":             detect.Email,
	"phone_number":      detect.Phone,
	"ssn":               detect.SSN,
	"credit_debit_card": detect.Card,
	"iban":              detect.IBAN,
}

// FromNemotron converts one Nemotron-PII row (nvidia/Nemotron-PII, CC-BY-4.0)
// to a Sample. Offsets in the dataset count characters; ours count bytes.
// Spans whose text does not match the offsets are dropped and counted.
func FromNemotron(text, spans string) (s synth.Sample, dropped int, err error) {
	s.Text = text
	v, err := parsePyLiteral(spans)
	if err != nil {
		return s, 0, err
	}
	list, ok := v.([]any)
	if !ok {
		return s, 0, fmt.Errorf("spans is not a list")
	}
	// byteAt[i] is the byte offset of character i; one extra entry for the end.
	byteAt := make([]int, 0, len(text)+1)
	for b := range text {
		byteAt = append(byteAt, b)
	}
	byteAt = append(byteAt, len(text))

	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			dropped++
			continue
		}
		start, e1 := asInt(m["start"])
		end, e2 := asInt(m["end"])
		label, _ := m["label"].(string)
		want := fmt.Sprint(m["text"]) // numeric-looking values arrive as numbers
		if e1 != nil || e2 != nil || start < 0 || end > len(byteAt)-1 || start >= end {
			dropped++
			continue
		}
		bs, be := byteAt[start], byteAt[end]
		// The dataset's own copy of the text is sometimes case-normalised
		// ("black" for "Black"), so compare case-insensitively; the offsets are
		// what matters.
		if !strings.EqualFold(text[bs:be], want) {
			dropped++
			continue
		}
		kind, inScope := nemotronKinds[label]
		if !inScope {
			kind = KindOther
		}
		s.Spans = append(s.Spans, synth.Span{Kind: kind, Start: bs, End: be})
	}
	return s, dropped, nil
}

func asInt(v any) (int, error) {
	switch n := v.(type) {
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case json.Number:
		i, err := n.Int64()
		return int(i), err
	}
	return 0, fmt.Errorf("not a number: %v", v)
}
