package detect

import "regexp"

// Two shapes: international with a leading +, and North American numbers
// with or without a country code. Numbers written without a + in other
// national formats ("020 7946 0958") are misses by design: without a region
// hint they are indistinguishable from IDs.
var phoneRE = regexp.MustCompile(
	`\+[0-9]{1,3}(?:[ .\-]?\(?[0-9]{1,4}\)?){2,5}` +
		`|(?:1[ .\-]?)?(?:\([0-9]{3}\)|[0-9]{3})[ .\-]?[0-9]{3}[ .\-]?[0-9]{4}`)

func findPhones(s string) []Match {
	var out []Match
	// A rejected candidate may have swallowed the start of a real number (the
	// optional "1" country code glued to a preceding digit group), so retry from
	// the next byte instead of skipping the whole candidate.
	for pos := 0; pos < len(s); {
		r := phoneRE.FindStringIndex(s[pos:])
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

// acceptPhone validates s[start:end]. The international pattern is greedy and
// can run into digits that follow the number ("+1 415 555 2671 2024"), so on
// failure it retries shorter spans that end at a separator.
func acceptPhone(s string, start, end int) (int, bool) {
	if phoneBoundsOK(s, start, end) && validPhone(s[start:end]) {
		return end, true
	}
	for e := end - 1; e > start; e-- {
		if c := s[e]; c != ' ' && c != '.' && c != '-' {
			continue
		}
		if p := s[e-1]; !isDigit(p) && p != ')' {
			continue
		}
		if phoneBoundsOK(s, start, e) && validPhone(s[start:e]) {
			return e, true
		}
	}
	return 0, false
}

func phoneBoundsOK(s string, start, end int) bool {
	if !boundedAt(s, start, end) || hyphenJoined(s, start, end) {
		return false
	}
	// A leading "(" is not a word character, so check what precedes it too.
	return !(s[start] == '(' && start > 0 && isWord(s[start-1]))
}

func validPhone(text string) bool {
	digits := make([]byte, 0, 16)
	plus := text[0] == '+'
	for i := 0; i < len(text); i++ {
		if isDigit(text[i]) {
			digits = append(digits, text[i])
		}
	}
	n := len(digits)
	if n > 15 {
		return false // E.164 maximum
	}
	if plus && digits[0] == '1' {
		return n == 11 && nanp(digits[1:]) // country code 1 is always NANP
	}
	if digits[0] == '1' && n == 11 {
		return nanp(digits[1:])
	}
	if plus {
		return n >= 8 && digits[0] != '0'
	}
	return n == 10 && nanp(digits)
}

// nanp checks a 10-digit North American number: area code and exchange both
// start with 2-9. This alone rejects Unix epoch seconds (start with 1) and
// most 10-digit IDs starting with 0 or 1; IDs starting 2-9 still slip through,
// and the README miss table says so.
func nanp(d []byte) bool {
	return len(d) == 10 && d[0] >= '2' && d[3] >= '2'
}
