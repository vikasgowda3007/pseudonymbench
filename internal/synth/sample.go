package synth

import (
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
)

// Span is a gold label: a kind and a byte range of Sample.Text.
type Span struct {
	Kind  detect.Kind `json:"kind"`
	Start int         `json:"start"`
	End   int         `json:"end"`
}

// Sample is one labelled prompt.
type Sample struct {
	Text  string `json:"text"`
	Spans []Span `json:"spans"`
	// Negatives names the look-alikes in the text, for error analysis.
	Negatives []string `json:"negatives,omitempty"`
}

var sentences = map[detect.Kind][]string{
	detect.Email: {
		"Please send the invoice to {} before Friday.",
		"My work address is {} if you need it.",
		"Forward this thread to {} and copy the team.",
		"Reach out to {}, they handled the original request.",
	},
	detect.Phone: {
		"You can call me on {} after lunch.",
		"The front desk number is {}.",
		"Text {} when the driver arrives.",
		"Her mobile is {}, but she rarely answers.",
	},
	detect.SSN: {
		"For the background check my SSN is {}.",
		"The form asks for a social security number: {}.",
		"Employee record shows {} as the tax identifier.",
	},
	detect.Card: {
		"Charge it to my card {} please.",
		"The card on file ends up being {} with expiry 04/29.",
		"Refund the payment made with {}.",
	},
	detect.IBAN: {
		"Wire the funds to {} by Monday.",
		"Our account for transfers is {}.",
		"Please verify that {} is the right IBAN.",
	},
}

var fillers = []string{
	"Thanks for the quick turnaround on this.",
	"Let me know if anything looks off.",
	"I'll review the summary tomorrow morning.",
	"Can you also draft a short reply to the customer?",
	"The meeting moved to the third floor.",
	"Please keep the tone friendly but brief.",
}

// Sample builds a prompt with the given kinds, each as a sentence, plus
// filler and (when withNegatives) one or two look-alikes. Spans are exact.
func (g *Gen) Sample(kinds []detect.Kind, withNegatives bool) Sample {
	var b strings.Builder
	var s Sample
	add := func(text string) {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(text)
	}
	for _, k := range kinds {
		tpl := g.pick(sentences[k])
		val := g.Value(k)
		pre, post, _ := strings.Cut(tpl, "{}")
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(pre)
		start := b.Len()
		b.WriteString(val)
		s.Spans = append(s.Spans, Span{Kind: k, Start: start, End: b.Len()})
		b.WriteString(post)
		if g.R.Intn(2) == 0 {
			add(g.pick(fillers))
		}
	}
	if withNegatives {
		for n := 1 + g.R.Intn(2); n > 0; n-- {
			neg := g.Negative()
			add("Note: " + neg.Text + ".")
			s.Negatives = append(s.Negatives, neg.Name)
		}
	}
	if b.Len() == 0 {
		add(g.pick(fillers))
	}
	s.Text = b.String()
	return s
}

// Dataset returns n samples. A quarter have no PII at all (negatives only), so
// the false positive rate is measured on clean text too.
func (g *Gen) Dataset(n int) []Sample {
	out := make([]Sample, 0, n)
	for i := 0; i < n; i++ {
		if i%4 == 3 {
			out = append(out, g.Sample(nil, true))
			continue
		}
		var kinds []detect.Kind
		for c := 1 + g.R.Intn(3); c > 0; c-- {
			kinds = append(kinds, detect.Kinds[g.R.Intn(len(detect.Kinds))])
		}
		out = append(out, g.Sample(kinds, g.R.Intn(2) == 0))
	}
	return out
}
