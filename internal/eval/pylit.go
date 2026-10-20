package eval

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// parsePyLiteral parses the small subset of Python literal syntax that the
// Nemotron-PII "spans" column uses: lists, dicts, strings, numbers, None, True
// and False. The column holds a Python repr, not JSON (single-quoted strings,
// None), so encoding/json cannot read it.
func parsePyLiteral(s string) (any, error) {
	p := &pyParser{s: s}
	p.skipSpace()
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.i != len(p.s) {
		return nil, fmt.Errorf("pyliteral: trailing text at %d", p.i)
	}
	return v, nil
}

type pyParser struct {
	s string
	i int
}

func (p *pyParser) skipSpace() {
	for p.i < len(p.s) && strings.IndexByte(" \t\r\n", p.s[p.i]) >= 0 {
		p.i++
	}
}

func (p *pyParser) value() (any, error) {
	if p.i >= len(p.s) {
		return nil, fmt.Errorf("pyliteral: unexpected end")
	}
	switch c := p.s[p.i]; {
	case c == '[':
		return p.list()
	case c == '{':
		return p.dict()
	case c == '\'' || c == '"':
		return p.str()
	case c == '-' || (c >= '0' && c <= '9'):
		return p.number()
	default:
		for _, w := range []struct {
			text string
			val  any
		}{{"None", nil}, {"True", true}, {"False", false}} {
			if strings.HasPrefix(p.s[p.i:], w.text) {
				p.i += len(w.text)
				return w.val, nil
			}
		}
		return nil, fmt.Errorf("pyliteral: unexpected %q at %d", c, p.i)
	}
}

func (p *pyParser) list() (any, error) {
	p.i++ // [
	out := []any{}
	for {
		p.skipSpace()
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return out, nil
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.skipSpace()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
		}
	}
}

func (p *pyParser) dict() (any, error) {
	p.i++ // {
	out := map[string]any{}
	for {
		p.skipSpace()
		if p.i < len(p.s) && p.s[p.i] == '}' {
			p.i++
			return out, nil
		}
		k, err := p.value()
		if err != nil {
			return nil, err
		}
		key, ok := k.(string)
		if !ok {
			return nil, fmt.Errorf("pyliteral: non-string dict key %v", k)
		}
		p.skipSpace()
		if p.i >= len(p.s) || p.s[p.i] != ':' {
			return nil, fmt.Errorf("pyliteral: expected ':' at %d", p.i)
		}
		p.i++
		p.skipSpace()
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out[key] = v
		p.skipSpace()
		if p.i < len(p.s) && p.s[p.i] == ',' {
			p.i++
		}
	}
}

func (p *pyParser) number() (any, error) {
	start := p.i
	for p.i < len(p.s) && strings.IndexByte("+-0123456789.eE", p.s[p.i]) >= 0 {
		p.i++
	}
	text := p.s[start:p.i]
	if n, err := strconv.ParseInt(text, 10, 64); err == nil {
		return n, nil
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return nil, fmt.Errorf("pyliteral: bad number %q", text)
	}
	return f, nil
}

func (p *pyParser) str() (any, error) {
	quote := p.s[p.i]
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == quote:
			p.i++
			return b.String(), nil
		case c == '\\' && p.i+1 < len(p.s):
			n := p.s[p.i+1]
			p.i += 2
			switch n {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\\', '\'', '"':
				b.WriteByte(n)
			case 'x', 'u', 'U':
				width := map[byte]int{'x': 2, 'u': 4, 'U': 8}[n]
				if p.i+width > len(p.s) {
					return nil, fmt.Errorf("pyliteral: short escape")
				}
				cp, err := strconv.ParseUint(p.s[p.i:p.i+width], 16, 32)
				if err != nil {
					return nil, fmt.Errorf("pyliteral: bad escape: %w", err)
				}
				b.WriteRune(rune(cp))
				p.i += width
			default: // unknown escapes are kept as written, like Python does
				b.WriteByte('\\')
				b.WriteByte(n)
			}
		default:
			r, size := utf8.DecodeRuneInString(p.s[p.i:])
			b.WriteRune(r)
			p.i += size
		}
	}
	return nil, fmt.Errorf("pyliteral: unterminated string")
}
