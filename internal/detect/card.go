package detect

import "strconv"

// A card candidate is a run of digit groups joined by single spaces or
// hyphens. The detector tries every window of whole groups holding 13 to 19
// digits and accepts the first that passes the separator, Luhn and issuer
// checks. Windows never start or end inside a group, so a 20-digit order ID is
// not mined for a card number.
type group struct{ start, end int } // byte span of one digit group

func findCards(s string) []Match {
	var out []Match
	for i := 0; i < len(s); {
		if !isDigit(s[i]) {
			i++
			continue
		}
		// A run is a digit followed by any number of (optional single space or
		// hyphen, digit) pairs: the regex `[0-9](?:[ -]?[0-9])*`.
		j := i + 1
		for j < len(s) {
			if isDigit(s[j]) {
				j++
			} else if (s[j] == ' ' || s[j] == '-') && j+1 < len(s) && isDigit(s[j+1]) {
				j += 2
			} else {
				break
			}
		}
		out = append(out, cardsInRun(s, i, j)...)
		i = j
	}
	return out
}

func cardsInRun(s string, from, to int) []Match {
	var groups []group
	gs := from
	for i := from; i <= to; i++ {
		if i == to || !isDigit(s[i]) {
			groups = append(groups, group{gs, i})
			gs = i + 1
		}
	}
	var out []Match
	for i := 0; i < len(groups); {
		digits := 0
		found := -1
		for j := i; j < len(groups); j++ {
			digits += groups[j].end - groups[j].start
			if digits > 19 {
				break
			}
			if digits >= 13 && cardWindowOK(s, groups[i:j+1]) {
				found = j // keep extending: prefer the longest valid window
			}
		}
		if found < 0 {
			i++
			continue
		}
		start, end := groups[i].start, groups[found].end
		if boundedAt(s, start, end) {
			out = append(out, Match{Kind: Card, Start: start, End: end})
			i = found + 1
			continue
		}
		i++
	}
	return out
}

func cardWindowOK(s string, g []group) bool {
	var sep byte
	buf := make([]byte, 0, 19)
	for k, gr := range g {
		if k > 0 {
			c := s[gr.start-1]
			if sep == 0 {
				sep = c
			} else if c != sep {
				return false
			}
		}
		buf = append(buf, s[gr.start:gr.end]...)
	}
	return luhnValid(buf) && plausibleIssuer(buf)
}

// luhnValid reports whether the ASCII digit string passes the Luhn check.
func luhnValid(d []byte) bool {
	sum, alt := 0, false
	for i := len(d) - 1; i >= 0; i-- {
		n := int(d[i] - '0')
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return len(d) > 0 && sum%10 == 0
}

// issuer describes one card network by numeric prefix range and valid lengths.
// Ranges follow the networks' public documentation. The ISO/IEC 7812-1 text
// itself could not be fetched while researching (HTTP 403), so treat the table
// as a pragmatic filter, not a standard. Networks missing here (RuPay, Mir,
// Elo, Hipercard, ...) are misses by design and appear in the README miss table.
type issuer struct {
	name    string
	plen    int // prefix length compared
	lo, hi  int // inclusive numeric range of the prefix
	lengths []int
}

var issuers = []issuer{
	{"visa", 1, 4, 4, []int{13, 16, 19}},
	{"mastercard", 2, 51, 55, []int{16}},
	{"mastercard-2", 4, 2221, 2720, []int{16}},
	{"amex", 2, 34, 34, []int{15}},
	{"amex", 2, 37, 37, []int{15}},
	{"discover", 4, 6011, 6011, []int{16, 17, 18, 19}},
	{"discover", 3, 644, 649, []int{16, 17, 18, 19}},
	{"discover", 2, 65, 65, []int{16, 17, 18, 19}},
	{"discover-2", 6, 622126, 622925, []int{16, 17, 18, 19}},
	{"diners", 3, 300, 305, []int{14, 15, 16, 17, 18, 19}},
	{"diners", 2, 36, 36, []int{14, 15, 16, 17, 18, 19}},
	{"diners", 2, 38, 39, []int{14, 15, 16, 17, 18, 19}},
	{"jcb", 4, 3528, 3589, []int{16, 17, 18, 19}},
	{"unionpay", 2, 62, 62, []int{16, 17, 18, 19}},
	{"maestro", 4, 5018, 5018, []int{13, 14, 15, 16, 17, 18, 19}},
	{"maestro", 4, 5020, 5020, []int{13, 14, 15, 16, 17, 18, 19}},
	{"maestro", 4, 5038, 5038, []int{13, 14, 15, 16, 17, 18, 19}},
	{"maestro", 4, 6304, 6304, []int{13, 14, 15, 16, 17, 18, 19}},
	{"maestro", 4, 6759, 6759, []int{13, 14, 15, 16, 17, 18, 19}},
	{"maestro", 4, 6761, 6763, []int{13, 14, 15, 16, 17, 18, 19}},
}

func plausibleIssuer(d []byte) bool {
	for _, is := range issuers {
		if len(d) < is.plen {
			continue
		}
		p, err := strconv.Atoi(string(d[:is.plen]))
		if err != nil || p < is.lo || p > is.hi {
			continue
		}
		for _, l := range is.lengths {
			if l == len(d) {
				return true
			}
		}
	}
	return false
}

// LuhnValid reports whether the digits of s, ignoring spaces and hyphens, pass
// the Luhn check. It is exported for the evaluation tools, which use it to
// explain misses on datasets whose card numbers were not generated with a
// valid check digit.
func LuhnValid(s string) bool {
	d := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case isDigit(c):
			d = append(d, c)
		case c == ' ' || c == '-':
		default:
			return false
		}
	}
	return luhnValid(d)
}
