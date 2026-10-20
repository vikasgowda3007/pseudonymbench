package mask

import (
	"context"
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/jsonx"
)

// Reset forgets URL state. Call it between strings that are not continuous
// text, such as separate JSON values.
func (r *Restorer) Reset() { r.guard = urlGuard{} }

// RestoreJSON restores tokens in every string of a non-streamed response body,
// except structural fields. Tool-call arguments are parsed as JSON when they
// are valid, so restored values are escaped correctly.
func (r *Restorer) RestoreJSON(ctx context.Context, body []byte) ([]byte, error) {
	v, err := jsonx.Decode(body)
	if err != nil {
		return nil, err
	}
	nv := r.restoreValue(ctx, v, "")
	return jsonx.Encode(nv)
}

func (r *Restorer) restoreValue(ctx context.Context, v any, key string) any {
	switch x := v.(type) {
	case string:
		if structuralSkip[key] {
			return x
		}
		if key == "arguments" {
			return r.restoreArguments(ctx, x)
		}
		r.Reset()
		return r.Text(ctx, x)
	case []any:
		for i, e := range x {
			x[i] = r.restoreValue(ctx, e, key)
		}
		return x
	case map[string]any:
		for k, e := range x {
			x[k] = r.restoreValue(ctx, e, k)
		}
		return x
	default:
		return v
	}
}

func (r *Restorer) restoreArguments(ctx context.Context, s string) string {
	if !strings.Contains(s, "<") && !hasEscapedOpener(s) {
		return s
	}
	v, err := jsonx.Decode([]byte(s))
	if err != nil {
		r.Reset()
		return r.Text(ctx, s) // not valid JSON: restore as plain text
	}
	out, err := jsonx.Encode(r.restoreData(ctx, v))
	if err != nil {
		return s
	}
	return string(out)
}

// restoreData restores every string and key inside parsed arguments. Here the
// structural skip list does not apply: the keys are whatever the tool schema
// defines.
func (r *Restorer) restoreData(ctx context.Context, v any) any {
	switch x := v.(type) {
	case string:
		r.Reset()
		return r.Text(ctx, x)
	case []any:
		for i, e := range x {
			x[i] = r.restoreData(ctx, e)
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			r.Reset()
			out[r.Text(ctx, k)] = r.restoreData(ctx, e)
		}
		return out
	default:
		return v
	}
}

// hasEscapedOpener reports whether s holds the JSON escape for "<" (a
// backslash, u, 003c, in either case). Tool arguments are JSON text, and some
// backends write the bracket that way; decoding the JSON turns it back into a
// real "<", so such strings must not be skipped.
func hasEscapedOpener(s string) bool {
	for i := 0; i+6 <= len(s); i++ {
		if s[i] == '\\' && s[i+1] == 'u' && strings.EqualFold(s[i+2:i+6], "003c") {
			return true
		}
	}
	return false
}
