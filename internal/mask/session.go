// Package mask turns PII in OpenAI-style chat JSON into tokens and back.
//
// Masking walks the request body and replaces PII in every string except a
// short list of structural fields (model, role, function names, binary data)
// that the API would reject if altered. Unknown fields are masked, not skipped:
// the safe default for a field the proxy has never heard of is to scan it.
package mask

import (
	"context"
	"sync"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/vault"
)

// maxPrefetch bounds how many vault calls a prefetch runs at once.
const maxPrefetch = 16

type valueKey struct {
	kind  detect.Kind
	value string
}

// Session is one request's view of a vault session. It remembers the tokens
// minted during masking so restoring them in the response needs no vault round
// trip; tokens from earlier requests in the same session fall back to the vault.
//
// A Session may be used from several goroutines during Prefetch; the proxy
// otherwise uses one per request from a single goroutine.
type Session struct {
	v  vault.Vault
	id string

	mu      sync.Mutex
	local   map[string]string   // token -> value
	byValue map[valueKey]string // (kind, value) -> token
}

// NewSession returns a Session for sid backed by v.
func NewSession(v vault.Vault, sid string) *Session {
	return &Session{v: v, id: sid, local: map[string]string{}, byValue: map[valueKey]string{}}
}

// ID returns the vault session id.
func (s *Session) ID() string { return s.id }

// Remote reports whether vault calls cross a network. When they do, masking
// asks for all of a request's tokens at once (Prefetch) instead of one round
// trip per value.
func (s *Session) Remote() bool {
	r, ok := s.v.(interface{ Remote() bool })
	return ok && r.Remote()
}

// Tokenize returns the token for value, allocating one if needed.
func (s *Session) Tokenize(ctx context.Context, kind detect.Kind, value string) (string, error) {
	k := valueKey{kind, value}
	s.mu.Lock()
	tok, ok := s.byValue[k]
	s.mu.Unlock()
	if ok {
		return tok, nil
	}
	tok, err := s.v.Tokenize(ctx, s.id, kind, value)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.local[tok] = value
	s.byValue[k] = tok
	s.mu.Unlock()
	return tok, nil
}

// Prefetch allocates tokens for every key concurrently (at most maxPrefetch at
// a time) so that the masking pass that follows finds them in memory. It
// returns the first error; the caller must treat that as a failed request.
func (s *Session) Prefetch(ctx context.Context, keys []valueKey) error {
	var (
		wg    sync.WaitGroup
		sem   = make(chan struct{}, maxPrefetch)
		errMu sync.Mutex
		first error
	)
	for _, k := range keys {
		errMu.Lock()
		stop := first != nil
		errMu.Unlock()
		if stop {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(k valueKey) {
			defer func() { <-sem; wg.Done() }()
			if _, err := s.Tokenize(ctx, k.kind, k.value); err != nil {
				errMu.Lock()
				if first == nil {
					first = err
				}
				errMu.Unlock()
			}
		}(k)
	}
	wg.Wait()
	return first
}

// Restore returns the value behind tok.
func (s *Session) Restore(ctx context.Context, tok string) (string, bool, error) {
	s.mu.Lock()
	v, ok := s.local[tok]
	s.mu.Unlock()
	if ok {
		return v, true, nil
	}
	v, ok, err := s.v.Restore(ctx, s.id, tok)
	if ok {
		s.mu.Lock()
		s.local[tok] = v
		s.mu.Unlock()
	}
	return v, ok, err
}
