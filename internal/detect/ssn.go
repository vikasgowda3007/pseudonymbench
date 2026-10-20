package detect

import (
	"regexp"
	"sort"
)

// Bare nine digits are far too common (order numbers, zip+4 without hyphen),
// so they count only right after a context word.
var ssnContextRE = regexp.MustCompile(
	`(?i)\b(?:ssn|social security(?: number| no\.?| #)?)[ :#\-]{0,3}([0-9]{9})\b`)

func isSSNSep(c byte) bool { return c == '-' || c == ' ' }

// separatedSSN reports whether s[i:i+11] has the shape AAA-GG-SSSS (hyphen or
// space, either may differ here; findSSNs requires them equal) with word
// boundaries on both sides: the regex `\b[0-9]{3}[- ][0-9]{2}[- ][0-9]{4}\b`.
func separatedSSN(s string, i int) bool {
	if i+11 > len(s) || (i > 0 && isWord(s[i-1])) || (i+11 < len(s) && isWord(s[i+11])) {
		return false
	}
	for k := 0; k < 11; k++ {
		c := s[i+k]
		switch k {
		case 3, 6:
			if !isSSNSep(c) {
				return false
			}
		default:
			if !isDigit(c) {
				return false
			}
		}
	}
	return true
}

func findSSNs(s string) []Match {
	var out []Match
	for i := 0; i+11 <= len(s); i++ {
		if !isDigit(s[i]) || !separatedSSN(s, i) {
			continue
		}
		end := i + 11
		if s[i+3] == s[i+6] && !hyphenJoined(s, i, end) && validSSN(s[i:i+3], s[i+4:i+6], s[i+7:end]) {
			out = append(out, Match{Kind: SSN, Start: i, End: end})
		}
		i = end - 1 // a regex match is consumed whether or not it validates
	}
	if containsFold(s, "ssn") || containsFold(s, "social") {
		for _, r := range ssnContextRE.FindAllStringSubmatchIndex(s, -1) {
			a, b := r[2], r[3]
			if validSSN(s[a:a+3], s[a+3:a+5], s[a+5:b]) {
				out = append(out, Match{Kind: SSN, Start: a, End: b})
			}
		}
	}
	return sortedUnique(out)
}

// containsFold reports whether s contains the lowercase ASCII word w, ignoring case.
func containsFold(s, w string) bool {
	for i := 0; i+len(w) <= len(s); i++ {
		j := 0
		for ; j < len(w); j++ {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != w[j] {
				break
			}
		}
		if j == len(w) {
			return true
		}
	}
	return false
}

// validSSN applies the structural rules for numbers the SSA issues: area is
// not 000 or 666; group is not 00; serial is not 0000 (SSA POMS RM 10201.035).
// Areas 900-999 are not issued as SSNs, but numbers shaped 9xx-7x-xxxx or
// 9xx-8x-xxxx are ITINs, which are taxpayer IDs and just as sensitive
// (Presidio's supported-entities doc gives that shape), so they pass; other 9xx
// numbers do not. Passing says "could be an SSN or ITIN", not "is one".
func validSSN(area, group, serial string) bool {
	if area == "000" || area == "666" {
		return false
	}
	if area[0] == '9' && group[0] != '7' && group[0] != '8' {
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
