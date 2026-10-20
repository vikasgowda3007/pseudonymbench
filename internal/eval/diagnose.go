package eval

import (
	"fmt"
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/synth"
)

// Misses returns the text of up to limit gold spans of kind that no region
// touched. Use it on public or synthetic data only: it prints span text.
func Misses(samples []synth.Sample, preds [][]Region, kind detect.Kind, limit int) []string {
	var out []string
	for i, s := range samples {
		for _, g := range s.Spans {
			if g.Kind != kind {
				continue
			}
			hit := false
			for _, r := range preds[i] {
				if overlaps(r.Start, r.End, g.Start, g.End) {
					hit = true
					break
				}
			}
			if !hit && len(out) < limit {
				out = append(out, s.Text[g.Start:g.End])
			}
		}
	}
	return out
}

// CardChecksum counts gold CARD spans that pass the Luhn check. Real card
// numbers always do; numbers invented by a text generator mostly do not, which
// caps what any checksum-based detector can score on such data.
func CardChecksum(samples []synth.Sample) (pass, total int) {
	for _, s := range samples {
		for _, g := range s.Spans {
			if g.Kind == detect.Card {
				total++
				if detect.LuhnValid(s.Text[g.Start:g.End]) {
					pass++
				}
			}
		}
	}
	return
}

// Diagnose renders misses and the card checksum rate as Markdown.
func Diagnose(samples []synth.Sample, preds [][]Region, limit int) string {
	var sb strings.Builder
	if pass, total := CardChecksum(samples); total > 0 {
		fmt.Fprintf(&sb, "Gold card numbers that pass the Luhn check: %d of %d (%.1f%%).\n\n", pass, total, 100*float64(pass)/float64(total))
	}
	for _, k := range detect.Kinds {
		if m := Misses(samples, preds, k, limit); len(m) > 0 {
			fmt.Fprintf(&sb, "Missed %s examples: %q\n\n", k, m)
		}
	}
	return sb.String()
}
