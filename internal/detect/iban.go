package detect

import "regexp"

// The regex only finds a start (two letters, two digits) and a generous tail.
// The span is then cut to the length the country code requires, so an IBAN
// followed by an uppercase word keeps the word out of the match.
var ibanRE = regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}(?: ?[A-Z0-9]){11,30}`)

// ibanLen maps country code to total IBAN length. Values come from the SWIFT
// IBAN registry as commonly published; the registry and ISO 13616 pages could
// not be fetched while researching (HTTP 403), so a wrong entry here would
// surface as a missed IBAN, never as a false mask.
var ibanLen = map[string]int{
	"AD": 24, "AE": 23, "AL": 28, "AT": 20, "AZ": 28, "BA": 20, "BE": 16, "BG": 22,
	"BH": 22, "BR": 29, "BY": 28, "CH": 21, "CR": 22, "CY": 28, "CZ": 24, "DE": 22,
	"DK": 18, "DO": 28, "EE": 20, "EG": 29, "ES": 24, "FI": 18, "FO": 18, "FR": 27,
	"GB": 22, "GE": 22, "GI": 23, "GL": 18, "GR": 27, "GT": 28, "HR": 21, "HU": 28,
	"IE": 22, "IL": 23, "IQ": 23, "IS": 26, "IT": 27, "JO": 30, "KW": 30, "KZ": 20,
	"LB": 28, "LC": 32, "LI": 21, "LT": 20, "LU": 20, "LV": 21, "MC": 27, "MD": 24,
	"ME": 22, "MK": 19, "MR": 27, "MT": 31, "MU": 30, "NL": 18, "NO": 15, "PK": 24,
	"PL": 28, "PS": 29, "PT": 25, "QA": 29, "RO": 24, "RS": 22, "SA": 24, "SC": 31,
	"SE": 24, "SI": 19, "SK": 24, "SM": 27, "ST": 25, "SV": 28, "TL": 23, "TN": 24,
	"TR": 26, "UA": 29, "VA": 22, "VG": 24, "XK": 20,
}

func findIBANs(s string) []Match {
	var out []Match
	for _, r := range ibanRE.FindAllStringIndex(s, -1) {
		want, ok := ibanLen[s[r[0]:r[0]+2]]
		if !ok {
			continue
		}
		end, compact, ok := ibanSpan(s, r[0], r[1], want)
		if !ok || (end < len(s) && isAlnum(s[end])) {
			continue
		}
		if mod97(compact) {
			out = append(out, Match{Kind: IBAN, Start: r[0], End: end})
		}
	}
	return out
}

// ibanSpan collects want alphanumeric characters from s[start:limit], skipping
// single spaces, and returns the end offset and the compact form.
func ibanSpan(s string, start, limit, want int) (int, []byte, bool) {
	compact := make([]byte, 0, want)
	i := start
	for i < limit && len(compact) < want {
		if s[i] != ' ' {
			compact = append(compact, s[i])
		}
		i++
	}
	return i, compact, len(compact) == want
}

// mod97 implements the ISO 7064 mod 97-10 check used by IBAN: move the first
// four characters to the end, map letters to 10..35, and require remainder 1.
func mod97(iban []byte) bool {
	rem := 0
	feed := func(c byte) {
		if isDigit(c) {
			rem = (rem*10 + int(c-'0')) % 97
		} else {
			rem = (rem*100 + int(c-'A') + 10) % 97
		}
	}
	for _, c := range iban[4:] {
		feed(c)
	}
	for _, c := range iban[:4] {
		feed(c)
	}
	return rem == 1
}
