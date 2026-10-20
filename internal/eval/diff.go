// Package eval scores PII masking from the outside. It never looks inside a
// proxy: it sends text through, reads back what the upstream received, and
// works out which parts changed. That makes the same measurement applicable
// to any proxy, not just this one.
package eval

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/token"
)

// Region is a stretch of the original text that a masker changed.
type Region struct {
	Start, End int           // byte offsets into the original text
	Kinds      []detect.Kind // kinds parsed from the placeholders that replaced it, if any
	Replaced   int           // number of placeholders (or 1 if the text was replaced by something else)
}

type word struct {
	text       string
	start, end int
}

func words(s string) []word {
	var out []word
	start := -1
	for i, r := range s {
		if unicode.IsSpace(r) {
			if start >= 0 {
				out = append(out, word{s[start:i], start, i})
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, word{s[start:], start, len(s)})
	}
	return out
}

// Regions finds what changed between original and masked text. It aligns the
// two as sequences of whitespace-separated words (longest common subsequence),
// treats each run of unmatched original words as one changed region, then
// trims characters the region shares with its replacement, so "a@b.co." turned
// into "<EMAIL_x>." reports the span "a@b.co" without the full stop.
func Regions(original, masked string) []Region {
	a, b := words(original), words(masked)
	n, m := len(a), len(b)
	// dp[i][j] = length of the LCS of a[i:] and b[j:].
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i].text == b[j].text {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}

	var out []Region
	i, j := 0, 0
	for i < n || j < m {
		if i < n && j < m && a[i].text == b[j].text {
			i++
			j++
			continue
		}
		// Collect one run of non-matching words on both sides.
		i0, j0 := i, j
		for i < n || j < m {
			if i < n && j < m && a[i].text == b[j].text {
				break
			}
			switch {
			case i < n && (j >= m || dp[i+1][j] >= dp[i][j+1]):
				i++
			default:
				j++
			}
		}
		if i == i0 {
			continue // pure insertion: nothing of the original was changed
		}
		start, end := a[i0].start, a[i-1].end
		repl := ""
		if j > j0 {
			repl = masked[b[j0].start:b[j-1].end]
		}
		start, end = trimShared(original, start, end, repl)
		kinds := placeholderKinds(repl)
		count := len(kinds)
		if n := countPlaceholders(repl); n > count {
			count = n
		}
		if count == 0 {
			count = 1
		}
		out = append(out, Region{Start: start, End: end, Kinds: kinds, Replaced: count})
	}
	return out
}

// trimShared shrinks original[start:end] by the prefix and suffix it has in
// common with repl, never splitting a rune.
func trimShared(original string, start, end int, repl string) (int, int) {
	seg := original[start:end]
	p := 0
	for p < len(seg) && p < len(repl) && seg[p] == repl[p] {
		p++
	}
	for p > 0 && p < len(seg) && !utf8.RuneStart(seg[p]) {
		p--
	}
	limit := min(len(seg), len(repl)) - p
	s := 0
	for s < limit && seg[len(seg)-1-s] == repl[len(repl)-1-s] {
		s++
	}
	for s > 0 && !utf8.RuneStart(seg[len(seg)-s]) {
		s--
	}
	if p+s >= len(seg) {
		return start, end // fully shared: keep the region rather than emptying it
	}
	return start + p, end - s
}

var genericPlaceholder = regexp.MustCompile(
	`(?:<<?|\[\[?|\{\{?|<\|)\s*([A-Za-z][A-Za-z_ -]{1,24}?)(?:[_ -]?(?:[0-9]+|[0-9a-fA-F]{4,12}))?\s*(?:>>?|\]\]?|\}\}?|\|>)`)

// kindAliases maps placeholder labels used by various tools to our kinds.
var kindAliases = map[string]detect.Kind{
	"EMAIL": detect.Email, "EMAIL_ADDRESS": detect.Email, "EMAILADDRESS": detect.Email, "MAIL": detect.Email,
	"PHONE": detect.Phone, "PHONE_NUMBER": detect.Phone, "PHONENUMBER": detect.Phone, "TEL": detect.Phone,
	"SSN": detect.SSN, "US_SSN": detect.SSN, "SOCIAL_SECURITY_NUMBER": detect.SSN, "SOCIALSECURITYNUMBER": detect.SSN,
	"CARD": detect.Card, "CREDIT_CARD": detect.Card, "CREDITCARD": detect.Card, "CREDIT_CARD_NUMBER": detect.Card,
	"IBAN": detect.IBAN, "IBAN_CODE": detect.IBAN,
}

func normalizeLabel(s string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "_", "-", "_").Replace(strings.TrimSpace(s)))
}

// placeholderKinds returns the kinds of the placeholders in s, in order.
// Our own tokens are recognised exactly; other tools' placeholders (for
// example <EMAIL_ADDRESS>, [PHONE_NUMBER], {{SSN_1}}) through a small alias table.
func placeholderKinds(s string) []detect.Kind {
	var kinds []detect.Kind
	for _, loc := range genericPlaceholder.FindAllStringSubmatchIndex(s, -1) {
		label := normalizeLabel(s[loc[2]:loc[3]])
		if k, ok := kindAliases[label]; ok {
			kinds = append(kinds, k)
		}
	}
	return kinds
}

func countPlaceholders(s string) int {
	n := len(token.Pattern.FindAllStringIndex(s, -1))
	if g := len(genericPlaceholder.FindAllStringIndex(s, -1)); g > n {
		n = g
	}
	return n
}
