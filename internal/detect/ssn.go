package detect

import (
	"regexp"
	"sort"
)

// Separated form: AAA-GG-SSSS or AAA GG SSSS with the same separator twice.
var ssnRE = regexp.MustCompile(`\b[0-9]{3}[- ][0-9]{2}[- ][0-9]{4}\b`)

// Bare nine digits are far too common (order numbers, zip+4 without hyphen),
// so they count only right after a context word.
var ssnContextRE = regexp.MustCompile(
	`(?i)\b(?:ssn|social security(?: number| no\.?| #)?)[ :#\-]{0,3}([0-9]{9})\b`)

func findSSNs(s string) []Match {
	var out []Match
	for _, r := range ssnRE.FindAllStringIndex(s, -1) {
		if s[r[0]+3] != s[r[0]+6] || hyphenJoined(s, r[0], r[1]) {
			continue
		}
		if validSSN(s[r[0]:r[0]+3], s[r[0]+4:r[0]+6], s[r[0]+7:r[1]]) {
			out = append(out, Match{Kind: SSN, Start: r[0], End: r[1]})
		}
	}
	for _, r := range ssnContextRE.FindAllStringSubmatchIndex(s, -1) {
		a, b := r[2], r[3]
		if validSSN(s[a:a+3], s[a+3:a+5], s[a+5:b]) {
			out = append(out, Match{Kind: SSN, Start: a, End: b})
		}
	}
	return sortedUnique(out)
}

// validSSN applies the structural rules for numbers the SSA issues: area is
// not 000, 666 or 900-999; group is not 00; serial is not 0000 (SSA POMS
// RM 10201.035). Numbers assigned today are random within these limits, so
// passing says "could be an SSN", not "is one". ITINs start with 9 and are
// rejected, a known miss.
func validSSN(area, group, serial string) bool {
	if area == "000" || area == "666" || area[0] == '9' {
		return false
	}
	return group != "00" && serial != "0000"
}

// sortedUnique orders matches by start and drops duplicates and overlaps,
// keeping the earlier span. Needed because the separated and contextual forms
// are found by different regexes.
func sortedUnique(in []Match) []Match {
	if len(in) < 2 {
		return in
	}
	sort.Slice(in, func(i, j int) bool { return in[i].Start < in[j].Start })
	out := in[:1]
	for _, x := range in[1:] {
		if x.Start >= out[len(out)-1].End {
			out = append(out, x)
		}
	}
	return out
}
