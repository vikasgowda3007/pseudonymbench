// Package token defines the placeholder format that replaces PII in prompts:
// <KIND_xxxxxxxx>, where KIND is a detect.Kind and x is a lowercase hex digit.
// Everything here is pure string work shared by the masker and the stream
// rewriter, so the two can never disagree about what a token looks like.
package token

import (
	"crypto/rand"
	"regexp"
	"strconv"
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
)

const (
	// HexLen is the number of random hex digits in a token. Eight digits
	// (32 bits) keep per-session collisions rare; the vault still checks.
	HexLen = 8

	// MaxLen is the length of the longest possible token, "<PHONE_" + hex + ">".
	// The stream rewriter never holds back more than MaxLen-1 bytes.
	MaxLen = 1 + 5 + 1 + HexLen + 1
)

// Pattern matches one complete token.
var Pattern = regexp.MustCompile(`<(` + kindAlternation() + `)_[0-9a-f]{` + strconv.Itoa(HexLen) + `}>`)

func kindAlternation() string {
	names := make([]string, len(detect.Kinds))
	for i, k := range detect.Kinds {
		names[i] = string(k)
	}
	return strings.Join(names, "|")
}

// New returns a fresh random token for kind.
func New(kind detect.Kind) string {
	var b [HexLen / 2]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("token: crypto/rand failed: " + err.Error())
	}
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 0, MaxLen)
	out = append(out, '<')
	out = append(out, kind...)
	out = append(out, '_')
	for _, v := range b {
		out = append(out, hexdigits[v>>4], hexdigits[v&0xf])
	}
	out = append(out, '>')
	return string(out)
}

// Kind returns the kind encoded in a complete token.
func Kind(tok string) detect.Kind {
	return detect.Kind(tok[1:strings.IndexByte(tok, '_')])
}

// Valid reports whether s is exactly one complete token.
func Valid(s string) bool {
	loc := Pattern.FindStringIndex(s)
	return loc != nil && loc[0] == 0 && loc[1] == len(s)
}

// SplitTail splits s into a part that is safe to process now and a held tail
// that could still grow into a token with more input. Only the last '<' can
// start such a tail: a viable prefix contains no second '<'.
func SplitTail(s string) (safe, held string) {
	i := strings.LastIndexByte(s, '<')
	if i < 0 || len(s)-i >= MaxLen {
		return s, ""
	}
	if couldBecomeToken(s[i:]) {
		return s[:i], s[i:]
	}
	return s, ""
}

// couldBecomeToken reports whether p is a proper prefix of some complete token.
func couldBecomeToken(p string) bool {
	for _, k := range detect.Kinds {
		head := "<" + string(k) + "_"
		if len(p) <= len(head) {
			if strings.HasPrefix(head, p) {
				return true
			}
			continue
		}
		if !strings.HasPrefix(p, head) {
			continue
		}
		rest := p[len(head):]
		if len(rest) <= HexLen && isLowerHex(rest) {
			return true
		}
	}
	return false
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
