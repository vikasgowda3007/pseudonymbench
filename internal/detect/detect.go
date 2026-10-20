// Package detect finds PII in text with regexes plus checksum and structure
// validation. It does no I/O and keeps no state, so it is safe for concurrent
// use and for fuzzing.
//
// Go's regexp is RE2: linear time, no lookaround. Boundary checks that would
// need lookbehind are done in code after the regex finds a candidate.
package detect

import "sort"

// Kind names a class of PII.
type Kind string

const (
	Card  Kind = "CARD"
	IBAN  Kind = "IBAN"
	Email Kind = "EMAIL"
	SSN   Kind = "SSN"
	Phone Kind = "PHONE"
)

// Kinds lists every kind in resolution priority. When two candidates overlap,
// the earlier kind wins, most specific first: an IBAN must match a country
// length and pass mod-97 (a random string passes about 1 time in 97), a card
// only has to pass Luhn and an issuer prefix (about 1 in 10, then a prefix
// table), so a numeric IBAN that happens to hold a Luhn-valid window is an
// IBAN. Phone goes last because its shape is the loosest.
var Kinds = []Kind{IBAN, Card, Email, SSN, Phone}

// Match is a byte span of s. It carries no copy of the value, so printing or
// logging a Match cannot leak PII.
type Match struct {
	Kind  Kind
	Start int // inclusive
	End   int // exclusive
}

// Value returns the matched text of s.
func (m Match) Value(s string) string { return s[m.Start:m.End] }

// Set selects kinds. The zero value selects nothing; use AllKinds for all.
type Set map[Kind]bool

// AllKinds returns a Set with every kind enabled.
func AllKinds() Set {
	s := make(Set, len(Kinds))
	for _, k := range Kinds {
		s[k] = true
	}
	return s
}

var finders = map[Kind]func(string) []Match{
	Card:  findCards,
	IBAN:  findIBANs,
	Email: findEmails,
	SSN:   findSSNs,
	Phone: findPhones,
}

// Find returns the non-overlapping matches of the enabled kinds, sorted by
// start offset. Overlaps resolve by the priority order of Kinds.
func Find(s string, enabled Set) []Match {
	var out []Match
	for _, k := range Kinds {
		if !enabled[k] {
			continue
		}
		cand := finders[k](s)
		if len(cand) == 0 {
			continue
		}
		out = mergeSorted(out, dropOverlaps(cand, out))
	}
	return out
}

// dropOverlaps removes from cand every match that overlaps one in accepted.
// Both slices are sorted and internally non-overlapping.
func dropOverlaps(cand, accepted []Match) []Match {
	if len(accepted) == 0 {
		return cand
	}
	kept := cand[:0:0]
	j := 0
	for _, c := range cand {
		for j < len(accepted) && accepted[j].End <= c.Start {
			j++
		}
		if j < len(accepted) && accepted[j].Start < c.End {
			continue
		}
		kept = append(kept, c)
	}
	return kept
}

func mergeSorted(a, b []Match) []Match {
	if len(a) == 0 {
		return b
	}
	out := make([]Match, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isAlnum(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isWord(c byte) bool { return isAlnum(c) || c == '_' }

// boundedAt reports whether s[start:end] is not glued to a word character on
// either side.
func boundedAt(s string, start, end int) bool {
	if start > 0 && isWord(s[start-1]) {
		return false
	}
	if end < len(s) && isWord(s[end]) {
		return false
	}
	return true
}

// hyphenJoined reports whether the span continues into a longer
// hyphen-separated number, as in "555-12-3456-7890".
func hyphenJoined(s string, start, end int) bool {
	if end+1 < len(s) && s[end] == '-' && isDigit(s[end+1]) {
		return true
	}
	if start >= 2 && s[start-1] == '-' && isDigit(s[start-2]) {
		return true
	}
	return false
}
