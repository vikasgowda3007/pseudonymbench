package detect

import (
	"fmt"
	"strings"
	"testing"
)

// found renders the matches of s as "KIND:value" strings for easy comparison.
func found(s string) []string {
	var out []string
	for _, m := range Find(s, AllKinds()) {
		out = append(out, fmt.Sprintf("%s:%s", m.Kind, m.Value(s)))
	}
	return out
}

func TestFind(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"email in sentence", "Email john@acme.com about it.", []string{"EMAIL:john@acme.com"}},
		{"email trailing dot", "write to john.doe+tag@sub.acme.co.uk.", []string{"EMAIL:john.doe+tag@sub.acme.co.uk"}},
		{"email double dot rejected", "a..b@acme.com", nil},
		{"not an email", "meet @ noon, or user@localhost", nil},
		{"card visa", "card 4111111111111111 exp", []string{"CARD:4111111111111111"}},
		{"card spaces", "4111 1111 1111 1111", []string{"CARD:4111 1111 1111 1111"}},
		{"card hyphens", "4111-1111-1111-1111", []string{"CARD:4111-1111-1111-1111"}},
		{"card amex grouping", "3782 822463 10005", []string{"CARD:3782 822463 10005"}},
		{"card mastercard", "5555555555554444", []string{"CARD:5555555555554444"}},
		{"card discover", "6011111111111117", []string{"CARD:6011111111111117"}},
		{"card jcb", "3530111333300000", []string{"CARD:3530111333300000"}},
		{"card diners", "30569309025904", []string{"CARD:30569309025904"}},
		{"card bad luhn", "4111111111111112", nil},
		{"card mixed separators", "4111 1111-1111 1111", nil},
		{"card 20 digit id", "order 41111111111111111111", nil},
		{"card glued to letters", "ID4111111111111111", nil},
		{"unknown issuer", "1234567812345670", nil},
		{"ssn dashes", "ssn 078-05-1120 ok", []string{"SSN:078-05-1120"}},
		{"ssn spaces", "123 45 6789", []string{"SSN:123 45 6789"}},
		{"ssn area 000", "000-12-3456", nil},
		{"ssn area 666", "666-12-3456", nil},
		{"ssn area 9xx", "900-12-3456", nil},
		{"itin shape 7x", "912-70-1234", []string{"SSN:912-70-1234"}},
		{"itin shape 8x", "987 88 4321", []string{"SSN:987 88 4321"}},
		{"ssn group 00", "123-00-6789", nil},
		{"ssn serial 0000", "123-45-0000", nil},
		{"ssn mixed separators", "123-45 6789", nil},
		{"ssn bare needs context", "order 123456789 shipped", nil},
		{"ssn bare with context", "SSN: 123456789", []string{"SSN:123456789"}},
		{"ssn joined to longer number", "555-12-3456-7890", nil},
		{"phone parens", "call (415) 555-2671 now", []string{"PHONE:(415) 555-2671"}},
		{"phone dashes", "415-555-2671", []string{"PHONE:415-555-2671"}},
		{"phone plus one", "+1 415 555 2671", []string{"PHONE:+1 415 555 2671"}},
		{"phone one prefix", "1-800-555-0199", []string{"PHONE:1-800-555-0199"}},
		{"phone intl uk", "ring +44 20 7946 0958 please", []string{"PHONE:+44 20 7946 0958"}},
		{"phone intl compact", "+442079460958", []string{"PHONE:+442079460958"}},
		{"phone then trailing number", "call +1 415 555 2671 2024", []string{"PHONE:+1 415 555 2671"}},
		{"phone then parenthesised number", "+20000000(2002000000", []string{"PHONE:+20000000", "PHONE:2002000000"}},
		{"phone after glued digit group", "4111-1111 415-555-2671 x", []string{"PHONE:415-555-2671"}},
		{"phone epoch rejected", "ts=1729350000", nil},
		{"phone leading zero area", "012 345 6789", nil},
		{"phone too long", "+1234567890123456789", nil},
		{"iban gb", "pay GB82 WEST 1234 5698 7654 32 today", []string{"IBAN:GB82 WEST 1234 5698 7654 32"}},
		{"iban compact", "DE89370400440532013000", []string{"IBAN:DE89370400440532013000"}},
		{"iban de spaced", "DE89 3704 0044 0532 0130 00", []string{"IBAN:DE89 3704 0044 0532 0130 00"}},
		{"iban followed by uppercase word", "GB82 WEST 1234 5698 7654 32 THANKS", []string{"IBAN:GB82 WEST 1234 5698 7654 32"}},
		{"iban bad checksum", "GB83 WEST 1234 5698 7654 32", nil},
		{"iban unknown country", "ZZ82 WEST 1234 5698 7654 32", nil},
		{"iban fr", "FR14 2004 1010 0505 0001 3M02 606", []string{"IBAN:FR14 2004 1010 0505 0001 3M02 606"}},
		{"iban es", "ES91 2100 0418 4502 0005 1332", []string{"IBAN:ES91 2100 0418 4502 0005 1332"}},
		{"iban nl", "NL91ABNA0417164300", []string{"IBAN:NL91ABNA0417164300"}},
		{"iban ch", "CH93 0076 2011 6238 5295 7", []string{"IBAN:CH93 0076 2011 6238 5295 7"}},
		{"iban it", "IT60 X054 2811 1010 0000 0123 456", []string{"IBAN:IT60 X054 2811 1010 0000 0123 456"}},
		{"iban ie", "IE29 AIBK 9311 5212 3456 78", []string{"IBAN:IE29 AIBK 9311 5212 3456 78"}},
		{"iban be", "BE68 5390 0754 7034", []string{"IBAN:BE68 5390 0754 7034"}},
		{"iban at", "AT61 1904 3002 3457 3201", []string{"IBAN:AT61 1904 3002 3457 3201"}},
		{"iban pl", "PL61 1090 1014 0000 0712 1981 2874", []string{"IBAN:PL61 1090 1014 0000 0712 1981 2874"}},
		{"iban pt", "PT50 0002 0123 1234 5678 9015 4", []string{"IBAN:PT50 0002 0123 1234 5678 9015 4"}},
		{"iban se", "SE45 5000 0000 0583 9825 7466", []string{"IBAN:SE45 5000 0000 0583 9825 7466"}},
		{"iban no", "NO93 8601 1117 947", []string{"IBAN:NO93 8601 1117 947"}},
		{"several kinds", "Email john@acme.com about card 4111 1111 1111 1111, call 415-555-2671",
			[]string{"EMAIL:john@acme.com", "CARD:4111 1111 1111 1111", "PHONE:415-555-2671"}},
		{"numeric iban with luhn-valid window", "ES23 4953 4145 6514 5192 4681", []string{"IBAN:ES23 4953 4145 6514 5192 4681"}},
		{"card beats phone", "4111 1111 1111 1111", []string{"CARD:4111 1111 1111 1111"}},
		{"iban beats phone", "DE89 3704 0044 0532 0130 00", []string{"IBAN:DE89 3704 0044 0532 0130 00"}},
		{"plain text", "The quick brown fox jumps over 13 lazy dogs on 2026-10-19.", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := found(tc.in)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("Find(%q)\n got  %q\n want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDisabledKinds(t *testing.T) {
	s := "john@acme.com 415-555-2671"
	got := Find(s, Set{Email: true})
	if len(got) != 1 || got[0].Kind != Email {
		t.Fatalf("got %v, want only the email", got)
	}
	if got := Find(s, nil); len(got) != 0 {
		t.Fatalf("nil set matched %v", got)
	}
}

func TestMatchHasNoValue(t *testing.T) {
	// Match must stay a plain span so logging one cannot leak the value.
	s := "john@acme.com"
	m := Find(s, AllKinds())[0]
	if out := fmt.Sprintf("%+v", m); strings.Contains(out, "john") {
		t.Fatalf("Match prints the value: %s", out)
	}
}

func TestLuhn(t *testing.T) {
	valid := []string{"4111111111111111", "378282246310005", "5555555555554444", "79927398713"}
	for _, v := range valid {
		if !luhnValid([]byte(v)) {
			t.Errorf("luhnValid(%s) = false", v)
		}
	}
	for _, v := range []string{"4111111111111112", "79927398710", ""} {
		if luhnValid([]byte(v)) {
			t.Errorf("luhnValid(%q) = true", v)
		}
	}
}

// TestLuhnPassRate documents why Luhn alone is a weak filter: about one random
// digit string in ten passes. The issuer table is what keeps false positives low.
func TestLuhnPassRate(t *testing.T) {
	pass, total := 0, 20000
	x := uint64(88172645463325252)
	buf := make([]byte, 16)
	for i := 0; i < total; i++ {
		for j := range buf {
			x ^= x << 13
			x ^= x >> 7
			x ^= x << 17
			buf[j] = '0' + byte(x%10)
		}
		if luhnValid(buf) {
			pass++
		}
	}
	rate := float64(pass) / float64(total)
	if rate < 0.09 || rate > 0.11 {
		t.Fatalf("Luhn pass rate %.4f, expected about 0.10", rate)
	}
}

func FuzzFind(f *testing.F) {
	for _, s := range []string{
		"Email john@acme.com about card 4111 1111 1111 1111",
		"ssn 078-05-1120 call +44 20 7946 0958 GB82 WEST 1234 5698 7654 32",
		"4111 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1 1",
		"((((415))))555-2671 -- ++1 -1",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		prev := 0
		for _, m := range Find(s, AllKinds()) {
			if m.Start < prev || m.End <= m.Start || m.End > len(s) {
				t.Fatalf("bad span %+v (prev end %d, len %d) in %q", m, prev, len(s), s)
			}
			prev = m.End
		}
	})
}

// The profile prefilter must never change what Find returns.
func FuzzPrefilterIsInvisible(f *testing.F) {
	for _, s := range []string{
		"Email john@acme.com about card 4111 1111 1111 1111",
		"ssn 078-05-1120 call +44 20 7946 0958 GB82 WEST 1234 5698 7654 32",
		"SSN: 123456789", "no pii here", "12345678", "AB12 3456789012 3456",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		a, b := find(s, AllKinds(), true), find(s, AllKinds(), false)
		if len(a) != len(b) {
			t.Fatalf("prefilter changed the result for %q: %v vs %v", s, a, b)
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("prefilter changed the result for %q: %v vs %v", s, a, b)
			}
		}
	})
}

func BenchmarkFindProse(b *testing.B) {
	s := strings.Repeat("Summarize the main trade-offs between optimistic and pessimistic locking in a relational database. ", 20)
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Find(s, AllKinds())
	}
}

func BenchmarkFind(b *testing.B) {
	s := strings.Repeat("Hello, please email john@acme.com about order 20261019123456 and card 4111 1111 1111 1111. ", 20)
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Find(s, AllKinds())
	}
}
