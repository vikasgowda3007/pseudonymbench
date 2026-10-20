package eval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/synth"
)

// LoadJSONL reads labelled samples, one JSON object per line.
func LoadJSONL(path string) ([]synth.Sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []synth.Sample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for line := 1; sc.Scan(); line++ {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var s synth.Sample
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		out = append(out, s)
	}
	return out, sc.Err()
}

// SaveJSONL writes samples, one JSON object per line.
func SaveJSONL(path string, samples []synth.Sample) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, s := range samples {
		if err := enc.Encode(s); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// KindScore holds the counts and rates for one kind.
type KindScore struct {
	Gold         int     `json:"gold"`
	HitOverlap   int     `json:"hit_overlap"` // gold spans touched by a changed region
	HitExact     int     `json:"hit_exact"`   // gold spans whose boundaries a region matched exactly
	Predicted    int     `json:"predicted"`   // placeholders of this kind produced
	PredictedTP  int     `json:"predicted_tp"`
	RecallOver   float64 `json:"recall_overlap"`
	RecallExact  float64 `json:"recall_exact"`
	PrecisionTyp float64 `json:"precision_typed"` // PredictedTP / Predicted
}

// Report is the outcome of scoring one masker on one dataset.
type Report struct {
	Label   string                    `json:"label"`
	Samples int                       `json:"samples"`
	Errors  int                       `json:"errors"` // samples the masker rejected or that could not be scored
	Kinds   map[detect.Kind]KindScore `json:"kinds"`

	// Overall counts every changed region, typed or not.
	Neutral         int     `json:"neutral_regions"` // replacements that only touched out-of-scope PII
	Regions         int     `json:"regions"`
	RegionsTP       int     `json:"regions_tp"` // regions overlapping any gold span
	PrecisionAll    float64 `json:"precision_overall"`
	CleanSamples    int     `json:"clean_samples"` // samples with no gold PII
	CleanWithRegion int     `json:"clean_with_region"`
	CleanFPRate     float64 `json:"clean_fp_rate"` // CleanWithRegion / CleanSamples
}

// Score compares gold labels with what a masker changed. preds[i] are the
// changed regions of samples[i]; a nil entry with errs[i] set counts as an error.
func Score(label string, samples []synth.Sample, preds [][]Region, errs []bool) Report {
	rep := Report{Label: label, Kinds: map[detect.Kind]KindScore{}}
	for _, k := range detect.Kinds {
		rep.Kinds[k] = KindScore{}
	}
	for i, s := range samples {
		if errs != nil && errs[i] {
			rep.Errors++
			continue
		}
		rep.Samples++
		regions := preds[i]
		if len(s.Spans) == 0 {
			rep.CleanSamples++
			if len(regions) > 0 {
				rep.CleanWithRegion++
			}
		}
		for _, g := range s.Spans {
			if g.Kind == KindOther {
				continue // out of scope: neither a hit nor a miss
			}
			ks := rep.Kinds[g.Kind]
			ks.Gold++
			for _, r := range regions {
				if overlaps(r.Start, r.End, g.Start, g.End) {
					ks.HitOverlap++
					break
				}
			}
			for _, r := range regions {
				if r.Start == g.Start && r.End == g.End {
					ks.HitExact++
					break
				}
			}
			rep.Kinds[g.Kind] = ks
		}
		for _, r := range regions {
			n := max(r.Replaced, 1)
			touched := goldTouched(r, s.Spans)
			other := touched[KindOther]
			delete(touched, KindOther)
			inScope := 0
			for _, c := range touched {
				inScope += c
			}
			if inScope == 0 && other > 0 {
				rep.Neutral += n // changed text that is PII of a kind we do not target
				continue
			}
			rep.Regions += n
			rep.RegionsTP += min(n, inScope)
			// Typed precision: each placeholder of kind k counts as a prediction
			// of k, correct if the region overlaps a gold span of that kind.
			for _, k := range r.Kinds {
				ks := rep.Kinds[k]
				ks.Predicted++
				if touched[k] > 0 {
					ks.PredictedTP++
					touched[k]--
				}
				rep.Kinds[k] = ks
			}
		}
	}
	for k, ks := range rep.Kinds {
		ks.RecallOver = ratio(ks.HitOverlap, ks.Gold)
		ks.RecallExact = ratio(ks.HitExact, ks.Gold)
		ks.PrecisionTyp = ratio(ks.PredictedTP, ks.Predicted)
		rep.Kinds[k] = ks
	}
	rep.PrecisionAll = ratio(rep.RegionsTP, rep.Regions)
	rep.CleanFPRate = ratio(rep.CleanWithRegion, rep.CleanSamples)
	return rep
}

// goldTouched counts, per kind, the gold spans a region overlaps.
func goldTouched(r Region, gold []synth.Span) map[detect.Kind]int {
	m := map[detect.Kind]int{}
	for _, g := range gold {
		if overlaps(r.Start, r.End, g.Start, g.End) {
			m[g.Kind]++
		}
	}
	return m
}

func overlaps(a0, a1, b0, b1 int) bool { return a0 < b1 && b0 < a1 }

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// Markdown renders the report as a table, one row per kind.
func (r Report) Markdown() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "**%s**: %d samples", r.Label, r.Samples)
	if r.Errors > 0 {
		fmt.Fprintf(&sb, ", %d rejected or unscored", r.Errors)
	}
	sb.WriteString("\n\n| kind | gold | recall (overlap) | recall (exact span) | predicted | precision (typed) |\n|---|---:|---:|---:|---:|---:|\n")
	kinds := make([]detect.Kind, 0, len(r.Kinds))
	for k := range r.Kinds {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	for _, k := range kinds {
		ks := r.Kinds[k]
		prec := "n/a"
		if ks.Predicted > 0 {
			prec = fmt.Sprintf("%.3f", ks.PrecisionTyp)
		}
		rec, recx := "n/a", "n/a"
		if ks.Gold > 0 {
			rec, recx = fmt.Sprintf("%.3f", ks.RecallOver), fmt.Sprintf("%.3f", ks.RecallExact)
		}
		fmt.Fprintf(&sb, "| %s | %d | %s | %s | %d | %s |\n", k, ks.Gold, rec, recx, ks.Predicted, prec)
	}
	fmt.Fprintf(&sb, "\nOverall precision (any changed region overlapping a gold span): %.3f over %d replacements. ", r.PrecisionAll, r.Regions)
	fmt.Fprintf(&sb, "Clean text (no PII): %d of %d samples had something masked (%.1f%%).", r.CleanWithRegion, r.CleanSamples, 100*r.CleanFPRate)
	if r.Neutral > 0 {
		fmt.Fprintf(&sb, " %d further replacements touched only out-of-scope PII (names, addresses, ...) and are not counted.", r.Neutral)
	}
	sb.WriteString("\n")
	return sb.String()
}
