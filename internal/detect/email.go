package detect

import (
	"regexp"
	"strings"
)

// Practical ASCII addresses, not the full RFC 5322 grammar. Quoted local parts,
// comments and internationalised (non-ASCII) addresses are misses by design.
//
// The scan is anchored on '@': for each one, the local part is found by walking
// left over local-part characters and the domain is matched by a regex anchored
// at the '@'. That keeps the regex off the 99% of text that is not near an
// address. Results are identical to running the whole-text regex
// `\b[A-Za-z0-9][A-Za-z0-9._%+\-]*@(?:label\.)+[A-Za-z]{2,}\b`; ref_test.go
// keeps that version and a differential fuzz test compares the two.
var emailDomainRE = regexp.MustCompile(
	`^@(?:[A-Za-z0-9](?:[A-Za-z0-9\-]*[A-Za-z0-9])?\.)+[A-Za-z]{2,}\b`)

func isLocalChar(c byte) bool {
	return isAlnum(c) || c == '.' || c == '_' || c == '%' || c == '+' || c == '-'
}

func isDomainChar(c byte) bool { return isAlnum(c) || c == '.' || c == '-' }

func findEmails(s string) []Match {
	var out []Match
	lastEnd := 0 // the whole-text regex never starts a match inside the previous one
	for from := 0; from < len(s); {
		i := strings.IndexByte(s[from:], '@')
		if i < 0 {
			break
		}
		at := from + i
		from = at + 1

		l := at
		for l > lastEnd && isLocalChar(s[l-1]) {
			l--
		}
		// The local part must start on an alphanumeric after a word boundary:
		// the first such position in the run, as the regex would pick.
		start := -1
		for p := l; p < at; p++ {
			if isAlnum(s[p]) && (p == 0 || !isWord(s[p-1])) {
				start = p
				break
			}
		}
		if start < 0 {
			continue
		}
		r := at + 1
		for r < len(s) && isDomainChar(s[r]) {
			r++
		}
		loc := emailDomainRE.FindStringIndex(s[at:min(r+1, len(s))])
		if loc == nil {
			continue
		}
		end := at + loc[1]
		if emailOK(s[start:end]) {
			out = append(out, Match{Kind: Email, Start: start, End: end})
		}
		lastEnd = end
	}
	return out
}

func emailOK(addr string) bool {
	if len(addr) > 254 {
		return false
	}
	at := strings.LastIndexByte(addr, '@')
	local := addr[:at]
	if len(local) > 64 || strings.Contains(local, "..") || strings.HasSuffix(local, ".") {
		return false
	}
	return true
}
