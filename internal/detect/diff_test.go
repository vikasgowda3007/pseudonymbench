package detect

import (
	"math/rand"
	"strings"
	"testing"
)

type pair struct {
	name     string
	fast, rf func(string) []Match
}

var pairs = []pair{
	{"email", findEmails, refFindEmails},
	{"card", findCards, refFindCards},
	{"ssn", findSSNs, refFindSSNs},
	{"phone", findPhones, refFindPhones},
	{"iban", findIBANs, refFindIBANs},
}

func equalMatches(a, b []Match) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func checkAll(t *testing.T, s string) {
	t.Helper()
	for _, p := range pairs {
		if got, want := p.fast(s), p.rf(s); !equalMatches(got, want) {
			t.Fatalf("%s differs from the reference on %q\n fast %v\n ref  %v", p.name, s, got, want)
		}
	}
}

// Fragments chosen to sit on the edges of each detector's grammar.
var fragments = []string{
	"a@b.com", "john.doe+x@sub.acme.co.uk", "x_y@z.io", "a..b@c.com", ".a@b.co", "a@b", "a@b.c", "a@b.co1",
	"a@b.co-x.com", "a@b-.com", "@", "@@", "a@@b.com", "a@b.com.", "a@b.com@c.org", "mailto:a@b.com",
	" ", "  ", ".", "-", "_", ",", ";", "(", ")", "+", "\n", "é", "x",
	"415-555-2671", "(415) 555-2671", "(415)555-2671", "415.555.2671", "4155552671", "+1 415 555 2671",
	"1-800-555-0199", "1 800 555 0199", "18005550199", "+44 20 7946 0958", "+442079460958", "+1", "+12", "1", "11",
	"012 345 6789", "123 456 7890", "1234567890", "12345678901", "1234567890123456",
	"123-45-6789", "078-05-1120", "078 05 1120", "123-45 6789", "000-12-3456", "912-70-1234", "900-12-3456",
	"ssn ", "SSN: ", "Social Security Number ", "social security no. ", "123456789", "ssn123456789",
	"4111 1111 1111 1111", "4111-1111-1111-1111", "4111111111111111", "3782 822463 10005", "4111 1111 1111 1112",
	"41111111111111111111", "5555 5555 5555 4444", "2221 0000 0000 0009",
	"GB82 WEST 1234 5698 7654 32", "DE89370400440532013000", "DE89 3704 0044 0532 0130 00", "ES23 4953 4145 6514 5192 4681",
	"GB82 WEST 1234 5698 7654 32 THANKS", "XGB82 WEST 1234 5698 7654 32", "GB83 WEST 1234 5698 7654 32", "ZZ82 WEST 1234 5698 7654 32",
	"AB12 ABCD EFGH IJKL MNOP QRST UVWX YZ01 2345 6789", "NL91ABNA0417164300", "NO93 8601 1117 947", "AB12", "AB12 3",
	"0", "9", "12", "123", "1234", "20261019",
}

func TestFastScannersMatchReference(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 60000; i++ {
		var b strings.Builder
		for n := 1 + rng.Intn(9); n > 0; n-- {
			b.WriteString(fragments[rng.Intn(len(fragments))])
		}
		checkAll(t, b.String())
	}
}

func TestFastScannersMatchReferenceOnRandomBytes(t *testing.T) {
	rng := rand.New(rand.NewSource(8))
	const alphabet = "0123456789 -.+()@_aAbBsSnN\n1111GBDEXZ"
	for i := 0; i < 60000; i++ {
		n := rng.Intn(40)
		buf := make([]byte, n)
		for j := range buf {
			buf[j] = alphabet[rng.Intn(len(alphabet))]
		}
		checkAll(t, string(buf))
	}
}

func FuzzFastScannersMatchReference(f *testing.F) {
	for _, s := range fragments {
		f.Add(s)
	}
	f.Add("Email john@acme.com about card 4111 1111 1111 1111, call 415-555-2671 ssn 078-05-1120")
	f.Fuzz(func(t *testing.T, s string) { checkAll(t, s) })
}
