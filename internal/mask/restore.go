package mask

import (
	"context"
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/token"
)

// RestoreStats counts what a Restorer did. It holds no values.
type RestoreStats struct {
	Restored   map[detect.Kind]int // tokens replaced by their value
	Unknown    int                 // well-formed tokens not in the vault: left visible
	URLSkipped int                 // tokens inside a URL: left visible on purpose
	Errors     int                 // vault errors: token left visible
}

// Add folds o into s.
func (s *RestoreStats) Add(o RestoreStats) {
	for k, n := range o.Restored {
		if s.Restored == nil {
			s.Restored = map[detect.Kind]int{}
		}
		s.Restored[k] += n
	}
	s.Unknown += o.Unknown
	s.URLSkipped += o.URLSkipped
	s.Errors += o.Errors
}

// Restorer replaces tokens in text with the values they stand for.
//
// Tokens that appear inside a URL are left alone unless AllowURLs is set. A
// prompt-injected model can be told to write a token into a link, and
// restoring it would hand the real value to whoever controls that link. The
// check is best-effort: a URL is a whitespace-delimited run containing "://".
//
// A Restorer carries state between calls (whether the current run of
// non-space text is a URL), so text fed in pieces behaves like the same text
// fed whole. It is not safe for concurrent use.
type Restorer struct {
	Session   *Session
	AllowURLs bool
	Stats     RestoreStats

	guard urlGuard
}

// Text restores the tokens in s. s must not end in the middle of a token; use
// token.SplitTail when feeding streamed pieces.
func (r *Restorer) Text(ctx context.Context, s string) string {
	if !strings.Contains(s, "<") {
		r.guard.feed(s)
		return s
	}
	var b strings.Builder
	last := 0
	for _, loc := range token.Pattern.FindAllStringIndex(s, -1) {
		r.guard.feed(s[last:loc[0]])
		b.WriteString(s[last:loc[0]])
		tok := s[loc[0]:loc[1]]
		last = loc[1]

		if r.guard.inURL() && !r.AllowURLs {
			r.Stats.URLSkipped++
			b.WriteString(tok)
			r.guard.feed(tok)
			continue
		}
		val, ok, err := r.Session.Restore(ctx, tok)
		switch {
		case err != nil:
			r.Stats.Errors++
			b.WriteString(tok)
			r.guard.feed(tok)
		case !ok:
			r.Stats.Unknown++
			b.WriteString(tok)
			r.guard.feed(tok)
		default:
			if r.Stats.Restored == nil {
				r.Stats.Restored = map[detect.Kind]int{}
			}
			r.Stats.Restored[token.Kind(tok)]++
			b.WriteString(val)
			r.guard.feed(val)
		}
	}
	r.guard.feed(s[last:])
	b.WriteString(s[last:])
	return b.String()
}

// urlGuard tracks whether the text emitted so far ends inside a URL. It sees
// every byte the Restorer emits, in order, in O(1) state.
type urlGuard struct {
	url  bool    // current run of non-space text contains "://"
	tail [2]byte // last two bytes seen, to catch "://" split across pieces
	n    int     // bytes of tail that are valid (0..2)
}

func (g *urlGuard) inURL() bool { return g.url }

func (g *urlGuard) feed(s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			g.url = false
			g.n = 0
			continue
		case '/':
			if g.n == 2 && g.tail[0] == ':' && g.tail[1] == '/' {
				g.url = true
			}
		}
		g.tail[0], g.tail[1] = g.tail[1], c
		if g.n < 2 {
			g.n++
		}
	}
}
