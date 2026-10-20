package token

import (
	"strings"
	"testing"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
)

func TestNewIsValid(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range detect.Kinds {
		for i := 0; i < 200; i++ {
			tok := New(k)
			if !Valid(tok) {
				t.Fatalf("New(%s) = %q is not Valid", k, tok)
			}
			if Kind(tok) != k {
				t.Fatalf("Kind(%q) = %s, want %s", tok, Kind(tok), k)
			}
			if len(tok) > MaxLen {
				t.Fatalf("%q longer than MaxLen %d", tok, MaxLen)
			}
			seen[tok] = true
		}
	}
	if len(seen) != len(detect.Kinds)*200 {
		t.Fatalf("collision among %d random tokens", len(detect.Kinds)*200)
	}
}

func TestMaxLenIsTight(t *testing.T) {
	longest := 0
	for _, k := range detect.Kinds {
		if n := len(New(k)); n > longest {
			longest = n
		}
	}
	if longest != MaxLen {
		t.Fatalf("longest token is %d bytes, MaxLen says %d", longest, MaxLen)
	}
}

func TestValid(t *testing.T) {
	for s, want := range map[string]bool{
		"<EMAIL_7f3a9c21>":  true,
		"<SSN_00000000>":    true,
		"<EMAIL_7F3A9C21>":  false, // uppercase hex
		"<EMAIL_7f3a9c2>":   false,
		"<EMAIL_7f3a9c211>": false,
		"<NAME_7f3a9c21>":   false,
		"EMAIL_7f3a9c21":    false,
		" <EMAIL_7f3a9c21>": false,
	} {
		if got := Valid(s); got != want {
			t.Errorf("Valid(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestSplitTail(t *testing.T) {
	tests := []struct{ in, safe, held string }{
		{"hello", "hello", ""},
		{"hello <", "hello ", "<"},
		{"hello <EM", "hello ", "<EM"},
		{"hello <EMAIL_", "hello ", "<EMAIL_"},
		{"hello <EMAIL_7f3a", "hello ", "<EMAIL_7f3a"},
		{"hello <EMAIL_7f3a9c21", "hello ", "<EMAIL_7f3a9c21"},
		{"hello <EMAIL_7f3a9c21>", "hello <EMAIL_7f3a9c21>", ""}, // complete: nothing to hold
		{"a < b", "a < b", ""},
		{"<div>", "<div>", ""},
		{"<EMAIL_zz", "<EMAIL_zz", ""},
		{"<<EM", "<", "<EM"},
		{"x <PHONE_0123456789ab", "x <PHONE_0123456789ab", ""}, // too long to be a token
		{"<S", "", "<S"},
		{"<SSN", "", "<SSN"},
		{"<SSX", "<SSX", ""},
	}
	for _, tc := range tests {
		safe, held := SplitTail(tc.in)
		if safe != tc.safe || held != tc.held {
			t.Errorf("SplitTail(%q) = (%q, %q), want (%q, %q)", tc.in, safe, held, tc.safe, tc.held)
		}
		if safe+held != tc.in {
			t.Errorf("SplitTail(%q) lost bytes: %q + %q", tc.in, safe, held)
		}
	}
}

// Every proper prefix of every possible token must be held, and nothing the
// grammar rules out may be.
func TestSplitTailHoldsEveryPrefix(t *testing.T) {
	for _, k := range detect.Kinds {
		tok := New(k)
		for n := 1; n < len(tok); n++ {
			safe, held := SplitTail("x " + tok[:n])
			if safe != "x " || held != tok[:n] {
				t.Errorf("prefix %q not held: (%q, %q)", tok[:n], safe, held)
			}
		}
	}
}

func FuzzSplitTail(f *testing.F) {
	for _, s := range []string{"", "<", "a<EMAIL_7f", "<EMAIL_7f3a9c21>", "<<<<", strings.Repeat("<", 40)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		safe, held := SplitTail(s)
		if safe+held != s {
			t.Fatalf("lost bytes: %q + %q != %q", safe, held, s)
		}
		if len(held) >= MaxLen {
			t.Fatalf("held %d bytes, cap is %d", len(held), MaxLen-1)
		}
		if held != "" && !couldBecomeToken(held) {
			t.Fatalf("held %q cannot become a token", held)
		}
	})
}
