// Package synth generates synthetic PII and labelled prompts for benchmarks.
//
// Everything is derived from a seeded random source, so a dataset is
// reproducible from its seed. The values follow the same public rules the
// detectors check (Luhn, mod-97, SSA structure), which means recall on this
// set measures "does the detector implement its own rules", not how it fares
// on real-world text. Hard negatives are deliberately included so precision
// is not trivially 1.0. The README says so next to the numbers.
package synth

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
)

// Gen produces values. It is not safe for concurrent use.
type Gen struct{ R *rand.Rand }

// New returns a Gen seeded with seed.
func New(seed int64) *Gen { return &Gen{R: rand.New(rand.NewSource(seed))} }

func (g *Gen) pick(xs []string) string { return xs[g.R.Intn(len(xs))] }

func (g *Gen) digits(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('0' + g.R.Intn(10))
	}
	return string(b)
}

func (g *Gen) upper(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('A' + g.R.Intn(26))
	}
	return string(b)
}

var (
	firstNames = []string{"john", "mary", "alex", "priya", "chen", "sofia", "omar", "lena", "raj", "nora", "tom", "ines"}
	lastNames  = []string{"smith", "doe", "garcia", "patel", "nguyen", "kim", "silva", "novak", "haas", "ali"}
	domains    = []string{"acme.com", "example.org", "mail.example.net", "corp.acme.co.uk", "uni.edu", "shop.io", "fastmail.fm"}
)

// Email returns an address such as "mary.kim42@acme.com".
func (g *Gen) Email() string {
	sep := g.pick([]string{".", "_", "-", ""})
	local := g.pick(firstNames) + sep + g.pick(lastNames)
	if g.R.Intn(2) == 0 {
		local += g.digits(1 + g.R.Intn(3))
	}
	if g.R.Intn(6) == 0 {
		local += "+" + g.pick([]string{"news", "bills", "test"})
	}
	return local + "@" + g.pick(domains)
}

// Phone returns a North American number in one of several common layouts.
func (g *Gen) Phone() string {
	area := fmt.Sprintf("%d%s", 2+g.R.Intn(8), g.digits(2))
	exch := fmt.Sprintf("%d%s", 2+g.R.Intn(8), g.digits(2))
	line := g.digits(4)
	switch g.R.Intn(5) {
	case 0:
		return fmt.Sprintf("(%s) %s-%s", area, exch, line)
	case 1:
		return fmt.Sprintf("%s-%s-%s", area, exch, line)
	case 2:
		return fmt.Sprintf("+1 %s %s %s", area, exch, line)
	case 3:
		return fmt.Sprintf("%s.%s.%s", area, exch, line)
	default:
		return fmt.Sprintf("1-%s-%s-%s", area, exch, line)
	}
}

// SSN returns a number with valid structure in AAA-GG-SSSS form.
func (g *Gen) SSN() string {
	area := 1 + g.R.Intn(899)
	for area == 666 {
		area = 1 + g.R.Intn(899)
	}
	return fmt.Sprintf("%03d-%02d-%04d", area, 1+g.R.Intn(99), 1+g.R.Intn(9999))
}

func luhnCheckDigit(partial string) byte {
	sum, alt := 0, true // the check digit will sit to the right of partial
	for i := len(partial) - 1; i >= 0; i-- {
		n := int(partial[i] - '0')
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return byte('0' + (10-sum%10)%10)
}

// withLuhn appends the Luhn check digit to partial.
func withLuhn(partial string) string { return partial + string(luhnCheckDigit(partial)) }

type cardSpec struct {
	prefixes []string
	length   int
}

var cardSpecs = []cardSpec{
	{[]string{"4"}, 16},
	{[]string{"51", "52", "53", "54", "55", "2221", "2720"}, 16},
	{[]string{"34", "37"}, 15},
	{[]string{"6011", "65", "644"}, 16},
	{[]string{"3528", "3589"}, 16},
	{[]string{"36", "300"}, 14},
}

// Card returns a Luhn-valid number with a real issuer prefix, formatted as
// plain digits, space-grouped or hyphen-grouped.
func (g *Gen) Card() string {
	spec := cardSpecs[g.R.Intn(len(cardSpecs))]
	prefix := spec.prefixes[g.R.Intn(len(spec.prefixes))]
	num := withLuhn(prefix + g.digits(spec.length-len(prefix)-1))
	sep := g.pick([]string{"", " ", "-"})
	if sep == "" {
		return num
	}
	if len(num) == 15 {
		return strings.Join([]string{num[:4], num[4:10], num[10:]}, sep)
	}
	var parts []string
	for i := 0; i < len(num); i += 4 {
		parts = append(parts, num[i:min(i+4, len(num))])
	}
	return strings.Join(parts, sep)
}

type ibanSpec struct {
	country string
	bban    func(g *Gen) string
}

var ibanSpecs = []ibanSpec{
	{"GB", func(g *Gen) string { return g.upper(4) + g.digits(14) }},
	{"DE", func(g *Gen) string { return g.digits(18) }},
	{"NL", func(g *Gen) string { return g.upper(4) + g.digits(10) }},
	{"ES", func(g *Gen) string { return g.digits(20) }},
	{"BE", func(g *Gen) string { return g.digits(12) }},
	{"AT", func(g *Gen) string { return g.digits(16) }},
}

// IBAN returns an IBAN with a correct mod-97 check, compact or in groups of four.
func (g *Gen) IBAN() string {
	spec := ibanSpecs[g.R.Intn(len(ibanSpecs))]
	bban := spec.bban(g)
	iban := spec.country + fmt.Sprintf("%02d", 98-mod97Of(bban+spec.country+"00")) + bban
	if g.R.Intn(2) == 0 {
		return iban
	}
	var parts []string
	for i := 0; i < len(iban); i += 4 {
		parts = append(parts, iban[i:min(i+4, len(iban))])
	}
	return strings.Join(parts, " ")
}

func mod97Of(s string) int {
	rem := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= '0' && c <= '9' {
			rem = (rem*10 + int(c-'0')) % 97
		} else {
			rem = (rem*100 + int(c-'A') + 10) % 97
		}
	}
	return rem
}

// Value returns one generated value of kind.
func (g *Gen) Value(k detect.Kind) string {
	switch k {
	case detect.Email:
		return g.Email()
	case detect.Phone:
		return g.Phone()
	case detect.SSN:
		return g.SSN()
	case detect.Card:
		return g.Card()
	default:
		return g.IBAN()
	}
}

// NegativeCase is text that looks a bit like PII but is not.
type NegativeCase struct{ Name, Text string }

// Negative returns a look-alike. Some of these will fool a regex detector;
// that is the point, and the benchmark reports the false positive rate.
func (g *Gen) Negative() NegativeCase {
	switch g.R.Intn(16) {
	case 0:
		return NegativeCase{"order id 12 digits", "order " + g.digits(12)}
	case 1:
		return NegativeCase{"order id 20 digits", "ref " + g.digits(20)}
	case 2:
		return NegativeCase{"epoch seconds", "created at " + "17" + g.digits(8)}
	case 3:
		return NegativeCase{"epoch millis", "ts=" + "17" + g.digits(11)}
	case 4:
		return NegativeCase{"ups tracking", "tracking 1Z" + g.upper(2) + g.digits(3) + g.digits(10)}
	case 5:
		return NegativeCase{"luhn-valid unknown issuer", "id " + withLuhn("99"+g.digits(13))}
	case 6:
		return NegativeCase{"16 digits bad luhn", "code " + badLuhn(g, 16)}
	case 7:
		return NegativeCase{"zip plus four", "ZIP 94105-" + g.digits(4)}
	case 8:
		return NegativeCase{"date", fmt.Sprintf("due 20%02d-%02d-%02d", 20+g.R.Intn(10), 1+g.R.Intn(12), 1+g.R.Intn(28))}
	case 9:
		return NegativeCase{"ip address", fmt.Sprintf("host %d.%d.%d.%d", 10+g.R.Intn(200), g.R.Intn(256), g.R.Intn(256), 1+g.R.Intn(254))}
	case 10:
		return NegativeCase{"version", fmt.Sprintf("running v%d.%d.%d", g.R.Intn(5), g.R.Intn(20), g.R.Intn(50))}
	case 11:
		return NegativeCase{"price", fmt.Sprintf("total $%d,%03d.%02d", 1+g.R.Intn(99), g.R.Intn(1000), g.R.Intn(100))}
	case 12:
		return NegativeCase{"nine digit number", "invoice " + g.digits(9)}
	case 13:
		// Roughly one in ten random 16-digit strings starting 4 or 51-55 passes
		// Luhn and the issuer table. This is the false positive rate the
		// checksum cannot remove.
		prefix := g.pick([]string{"4", "51", "52", "53", "54", "55"})
		return NegativeCase{"16 digit id, card-like prefix", "ref " + prefix + g.digits(16-len(prefix))}
	case 14:
		return NegativeCase{"10 digit id", "case " + fmt.Sprintf("%d%s", 2+g.R.Intn(8), g.digits(9))}
	default:
		return NegativeCase{"uuid", fmt.Sprintf("id %s-%s-%s-%s-%s", g.hex(8), g.hex(4), g.hex(4), g.hex(4), g.hex(12))}
	}
}

func (g *Gen) hex(n int) string {
	const d = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = d[g.R.Intn(16)]
	}
	return string(b)
}

func badLuhn(g *Gen, n int) string {
	partial := "4" + g.digits(n-2)
	good := luhnCheckDigit(partial)
	return partial + string('0'+(good-'0'+1+byte(g.R.Intn(9)))%10)
}
