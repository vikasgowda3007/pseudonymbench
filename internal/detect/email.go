package detect

import (
	"regexp"
	"strings"
)

// Practical ASCII addresses, not the full RFC 5322 grammar. Quoted local parts,
// comments and internationalised (non-ASCII) addresses are misses by design.
var emailRE = regexp.MustCompile(
	`\b[A-Za-z0-9][A-Za-z0-9._%+\-]*@(?:[A-Za-z0-9](?:[A-Za-z0-9\-]*[A-Za-z0-9])?\.)+[A-Za-z]{2,}\b`)

func findEmails(s string) []Match {
	var out []Match
	for _, r := range emailRE.FindAllStringIndex(s, -1) {
		if emailOK(s[r[0]:r[1]]) {
			out = append(out, Match{Kind: Email, Start: r[0], End: r[1]})
		}
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
