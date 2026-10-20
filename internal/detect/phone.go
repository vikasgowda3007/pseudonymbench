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
	for _, r := range phoneRE.FindAllStringIndex(s, -1) {
		start, end := r[0], r[1]
		if !boundedAt(s, start, end) || hyphenJoined(s, start, end) {
			continue
		}
		// A leading "(" is not a word character, so check what precedes it too.
		if s[start] == '(' && start > 0 && isWord(s[start-1]) {
			continue
		}
		if validPhone(s[start:end]) {
			out = append(out, Match{Kind: Phone, Start: start, End: end})
		}
	}
	return out
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
