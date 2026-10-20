package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/synth"
)

// presidioKinds maps Presidio entity names to ours. Anything else (PERSON,
// LOCATION, DATE_TIME ...) becomes an untyped region: it counts against
// precision unless it lies over out-of-scope gold PII.
var presidioKinds = map[string]detect.Kind{
	"EMAIL_ADDRESS": detect.Email,
	"PHONE_NUMBER":  detect.Phone,
	"US_SSN":        detect.SSN,
	"US_ITIN":       detect.SSN,
	"CREDIT_CARD":   detect.Card,
	"IBAN_CODE":     detect.IBAN,
}

// PresidioRegions scores a Presidio analyzer through the same pipeline as the
// proxies: it posts each sample to POST {baseURL}/analyze and treats every
// result at or above minScore as a replaced region. Presidio reports character
// offsets, which are converted to byte offsets.
//
// This needs a running analyzer, for example
// ghcr.io/data-privacy-stack/presidio-analyzer (port 3000 inside the container).
func PresidioRegions(ctx context.Context, baseURL string, minScore float64, workers int, samples []synth.Sample) ([][]Region, []bool) {
	preds := make([][]Region, len(samples))
	errs := make([]bool, len(samples))
	client := &http.Client{Timeout: 60 * time.Second}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < max(workers, 1); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				regions, err := analyze(ctx, client, baseURL, minScore, samples[i].Text)
				if err != nil {
					errs[i] = true
					continue
				}
				preds[i] = regions
			}
		}()
	}
	for i := range samples {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return preds, errs
}

func analyze(ctx context.Context, c *http.Client, baseURL string, minScore float64, text string) ([]Region, error) {
	body, _ := json.Marshal(map[string]any{"text": text, "language": "en", "score_threshold": minScore})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/analyze", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("analyzer returned %d", resp.StatusCode)
	}
	var results []struct {
		EntityType string  `json:"entity_type"`
		Start      int     `json:"start"`
		End        int     `json:"end"`
		Score      float64 `json:"score"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, err
	}
	byteAt := make([]int, 0, len(text)+1)
	for b := range text {
		byteAt = append(byteAt, b)
	}
	byteAt = append(byteAt, len(text))

	var out []Region
	for _, r := range results {
		if r.Score < minScore || r.Start < 0 || r.End > len(byteAt)-1 || r.Start >= r.End {
			continue
		}
		reg := Region{Start: byteAt[r.Start], End: byteAt[r.End], Replaced: 1}
		if k, ok := presidioKinds[r.EntityType]; ok {
			reg.Kinds = []detect.Kind{k}
		}
		out = append(out, reg)
	}
	return mergeOverlapping(out), nil
}

// mergeOverlapping keeps one region per stretch of text: Presidio can report
// the same span under several entity types.
func mergeOverlapping(rs []Region) []Region {
	if len(rs) < 2 {
		return rs
	}
	// insertion sort by start; result lists are short
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && rs[j].Start < rs[j-1].Start; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
	out := rs[:1]
	for _, r := range rs[1:] {
		last := &out[len(out)-1]
		if r.Start < last.End {
			if r.End > last.End {
				last.End = r.End
			}
			// Prefer a typed region over an untyped one covering the same text.
			if len(last.Kinds) == 0 && len(r.Kinds) > 0 {
				last.Kinds = r.Kinds
			}
			continue
		}
		out = append(out, r)
	}
	return out
}
