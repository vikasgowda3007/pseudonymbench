package detect

import "regexp"

// Reference implementations: the original, plainly written regex versions of
// the detectors. The shipped scanners in email.go, card.go, ssn.go and phone.go
// are hand-written for speed; these exist so differential tests can check that
// the fast versions return exactly the same matches.

var refEmailRE = regexp.MustCompile(
	`\b[A-Za-z0-9][A-Za-z0-9._%+\-]*@(?:[A-Za-z0-9](?:[A-Za-z0-9\-]*[A-Za-z0-9])?\.)+[A-Za-z]{2,}\b`)

func refFindEmails(s string) []Match {
	var out []Match
	for _, r := range refEmailRE.FindAllStringIndex(s, -1) {
		if emailOK(s[r[0]:r[1]]) {
			out = append(out, Match{Kind: Email, Start: r[0], End: r[1]})
		}
	}
	return out
}

var refDigitRunRE = regexp.MustCompile(`[0-9](?:[ -]?[0-9])*`)

func refFindCards(s string) []Match {
	var out []Match
	for _, r := range refDigitRunRE.FindAllStringIndex(s, -1) {
		out = append(out, cardsInRun(s, r[0], r[1])...)
	}
	return out
}

var refSSNRE = regexp.MustCompile(`\b[0-9]{3}[- ][0-9]{2}[- ][0-9]{4}\b`)

var refSSNContextRE = regexp.MustCompile(
	`(?i)\b(?:ssn|social security(?: number| no\.?| #)?)[ :#\-]{0,3}([0-9]{9})\b`)

func refFindSSNs(s string) []Match {
	var out []Match
	for _, r := range refSSNRE.FindAllStringIndex(s, -1) {
		if s[r[0]+3] != s[r[0]+6] || hyphenJoined(s, r[0], r[1]) {
			continue
		}
		if validSSN(s[r[0]:r[0]+3], s[r[0]+4:r[0]+6], s[r[0]+7:r[1]]) {
			out = append(out, Match{Kind: SSN, Start: r[0], End: r[1]})
		}
	}
	for _, r := range refSSNContextRE.FindAllStringSubmatchIndex(s, -1) {
		a, b := r[2], r[3]
		if validSSN(s[a:a+3], s[a+3:a+5], s[a+5:b]) {
			out = append(out, Match{Kind: SSN, Start: a, End: b})
		}
	}
	return sortedUnique(out)
}

var refPhoneRE = regexp.MustCompile(
	`\+[0-9]{1,3}(?:[ .\-]?\(?[0-9]{1,4}\)?){2,5}` +
		`|(?:1[ .\-]?)?(?:\([0-9]{3}\)|[0-9]{3})[ .\-]?[0-9]{3}[ .\-]?[0-9]{4}`)

func refFindPhones(s string) []Match {
	var out []Match
	for pos := 0; pos < len(s); {
		r := refPhoneRE.FindStringIndex(s[pos:])
		if r == nil {
			break
		}
		start, end := pos+r[0], pos+r[1]
		if e, ok := acceptPhone(s, start, end); ok {
			out = append(out, Match{Kind: Phone, Start: start, End: e})
			pos = e
			continue
		}
		pos = start + 1
	}
	return out
}

var refIBANRE = regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}(?: ?[A-Z0-9]){11,30}`)

func refFindIBANs(s string) []Match {
	var out []Match
	for _, r := range refIBANRE.FindAllStringIndex(s, -1) {
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
