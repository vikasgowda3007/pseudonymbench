package mask

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/jsonx"
)

// Action says what to do with a kind of PII.
type Action int

const (
	// ActionMask replaces the value with a token (the default).
	ActionMask Action = iota
	// ActionBlock rejects the whole request.
	ActionBlock
	// ActionOff leaves the kind alone. Use it to turn a noisy detector off.
	ActionOff
)

// Policy maps each kind to an action. Kinds not listed are masked.
type Policy map[detect.Kind]Action

func (p Policy) action(k detect.Kind) Action {
	if a, ok := p[k]; ok {
		return a
	}
	return ActionMask
}

// ErrInvalidBody means the request body is not a JSON object. The proxy
// rejects it rather than forwarding text it could not scan.
var ErrInvalidBody = errors.New("mask: request body is not a JSON object")

// BlockedError is returned when a kind with ActionBlock is found. It names the
// kinds only, never the values.
type BlockedError struct{ Kinds []detect.Kind }

func (e *BlockedError) Error() string {
	names := make([]string, len(e.Kinds))
	for i, k := range e.Kinds {
		names[i] = string(k)
	}
	return "request contains blocked PII kinds: " + strings.Join(names, ", ")
}

// Report summarises one masking pass. It holds counts only.
type Report struct {
	Masked  map[detect.Kind]int // occurrences replaced by tokens
	Strings int                 // strings scanned
}

// Total returns the number of replaced occurrences.
func (r Report) Total() int {
	n := 0
	for _, c := range r.Masked {
		n += c
	}
	return n
}

// Masker rewrites request bodies according to a Policy.
type Masker struct {
	policy  Policy
	enabled detect.Set
}

// New returns a Masker for policy.
func New(policy Policy) *Masker {
	en := detect.Set{}
	for _, k := range detect.Kinds {
		if policy.action(k) != ActionOff {
			en[k] = true
		}
	}
	return &Masker{policy: policy, enabled: en}
}

// structuralSkip lists keys whose string values the API validates or that
// carry no free text. Altering them would break the request. messages[].name
// is here because the API restricts it to [a-zA-Z0-9_-], so a token would be
// rejected; the README miss table lists it.
var structuralSkip = map[string]bool{
	"model": true, "role": true, "type": true, "id": true, "object": true,
	"tool_call_id": true, "finish_reason": true, "detail": true, "format": true,
	"name": true, "file_data": true, "file_id": true, "data": true,
	"encoding_format": true, "reasoning_effort": true, "service_tier": true,
}

// structuralTop lists the top-level keys whose layout is the chat-completions
// schema. Any other top-level key, and "metadata", is user data: its keys are
// scanned as well as its values.
var structuralTop = map[string]bool{
	"messages": true, "tools": true, "functions": true, "function_call": true,
	"tool_choice": true, "response_format": true, "stop": true,
	"stream_options": true, "prediction": true, "audio": true, "modalities": true,
	"user": true, "model": true,
}

// Request masks PII in a chat-completions request body.
func (m *Masker) Request(ctx context.Context, sess *Session, body []byte) ([]byte, Report, error) {
	v, err := jsonx.Decode(body)
	if err != nil {
		return nil, Report{}, ErrInvalidBody
	}
	top, ok := v.(map[string]any)
	if !ok {
		return nil, Report{}, ErrInvalidBody
	}
	w := &walker{ctx: ctx, m: m, sess: sess, blocked: map[detect.Kind]bool{}}
	w.rep.Masked = map[detect.Kind]int{}
	masked := make(map[string]any, len(top))
	for k, val := range top {
		nk := k
		if !structuralTop[k] {
			// An unknown top-level field is user data: its name can carry PII too.
			var err error
			if nk, err = w.str(k); err != nil {
				return nil, w.rep, err
			}
		}
		nv, err := w.value(val, k, structuralTop[k])
		if err != nil {
			return nil, w.rep, err
		}
		masked[nk] = nv
	}
	if len(w.blocked) > 0 {
		return nil, w.rep, w.blockedError()
	}
	out, err := jsonx.Encode(masked)
	return out, w.rep, err
}

type walker struct {
	ctx     context.Context
	m       *Masker
	sess    *Session
	rep     Report
	blocked map[detect.Kind]bool
}

func (w *walker) blockedError() error {
	e := &BlockedError{}
	for k := range w.blocked {
		e.Kinds = append(e.Kinds, k)
	}
	sort.Slice(e.Kinds, func(i, j int) bool { return e.Kinds[i] < e.Kinds[j] })
	return e
}

// value masks v. key is the nearest enclosing object key; structural says the
// subtree follows the API schema (skip list applies, keys are not scanned).
func (w *walker) value(v any, key string, structural bool) (any, error) {
	switch x := v.(type) {
	case string:
		if structural {
			if structuralSkip[key] {
				return x, nil
			}
			if key == "url" && strings.HasPrefix(x, "data:") {
				return x, nil // inline media: not text, and large
			}
			if key == "arguments" {
				return w.arguments(x)
			}
		}
		return w.str(x)
	case []any:
		for i, e := range x {
			ne, err := w.value(e, key, structural)
			if err != nil {
				return nil, err
			}
			x[i] = ne
		}
		return x, nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			nk := k
			if !structural {
				var err error
				if nk, err = w.str(k); err != nil {
					return nil, err
				}
			}
			ne, err := w.value(e, k, structural)
			if err != nil {
				return nil, err
			}
			out[nk] = ne
		}
		return out, nil
	default:
		return v, nil // numbers, bools, null
	}
}

// arguments masks a tool call's arguments, which the API sends as a string
// holding JSON. Valid JSON is parsed and every string and key in it scanned,
// then re-encoded, so escaping stays correct. Anything else is scanned as text.
func (w *walker) arguments(s string) (any, error) {
	v, err := jsonx.Decode([]byte(s))
	if err != nil {
		return w.str(s)
	}
	nv, err := w.value(v, "", false)
	if err != nil {
		return nil, err
	}
	out, err := jsonx.Encode(nv)
	if err != nil {
		return nil, err
	}
	return string(out), nil
}

// str replaces every enabled PII match in s with a token.
func (w *walker) str(s string) (string, error) {
	w.rep.Strings++
	matches := detect.Find(s, w.m.enabled)
	if len(matches) == 0 {
		return s, nil
	}
	var b strings.Builder
	last := 0
	for _, mt := range matches {
		if w.m.policy.action(mt.Kind) == ActionBlock {
			w.blocked[mt.Kind] = true
			continue
		}
		tok, err := w.sess.Tokenize(w.ctx, mt.Kind, mt.Value(s))
		if err != nil {
			return "", fmt.Errorf("mask: vault: %w", err)
		}
		b.WriteString(s[last:mt.Start])
		b.WriteString(tok)
		last = mt.End
		w.rep.Masked[mt.Kind]++
	}
	b.WriteString(s[last:])
	return b.String(), nil
}
