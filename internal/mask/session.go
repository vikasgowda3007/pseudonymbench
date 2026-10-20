// Package mask turns PII in OpenAI-style chat JSON into tokens and back.
//
// Masking walks the request body and replaces PII in every string except a
// short list of structural fields (model, role, function names, binary data)
// that the API would reject if altered. Unknown fields are masked, not skipped:
// the safe default for a field the proxy has never heard of is to scan it.
package mask

import (
	"context"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/vault"
)

// Session is one request's view of a vault session. It remembers the tokens
// minted during masking so restoring them in the response needs no vault round
// trip; tokens from earlier requests in the same session fall back to the vault.
//
// A Session is not safe for concurrent use. The proxy uses one per request.
type Session struct {
	v     vault.Vault
	id    string
	local map[string]string
}

// NewSession returns a Session for sid backed by v.
func NewSession(v vault.Vault, sid string) *Session {
	return &Session{v: v, id: sid, local: map[string]string{}}
}

// ID returns the vault session id.
func (s *Session) ID() string { return s.id }

// Tokenize returns the token for value, allocating one if needed.
func (s *Session) Tokenize(ctx context.Context, kind detect.Kind, value string) (string, error) {
	tok, err := s.v.Tokenize(ctx, s.id, kind, value)
	if err != nil {
		return "", err
	}
	s.local[tok] = value
	return tok, nil
}

// Restore returns the value behind tok.
func (s *Session) Restore(ctx context.Context, tok string) (string, bool, error) {
	if v, ok := s.local[tok]; ok {
		return v, true, nil
	}
	v, ok, err := s.v.Restore(ctx, s.id, tok)
	if ok {
		s.local[tok] = v
	}
	return v, ok, err
}
