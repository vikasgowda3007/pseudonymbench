// Package vault stores the mapping between tokens and the PII they replaced.
//
// A vault is scoped by session id. The same (kind, value) in the same session
// always yields the same token until the session expires; a token minted in
// one session restores nothing in another.
package vault

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
)

// ErrSessionFull is returned when a session holds the maximum number of
// distinct values. Callers must fail closed: reject the request.
var ErrSessionFull = errors.New("vault: session is full")

// maxAllocAttempts bounds retries when a random token collides with an
// existing one. With 32-bit tokens a single retry is already rare.
const maxAllocAttempts = 8

// Vault maps PII values to tokens and back.
type Vault interface {
	// Tokenize returns the token standing for value within session sid,
	// allocating one if the session has none yet. It is safe for concurrent use.
	Tokenize(ctx context.Context, sid string, kind detect.Kind, value string) (string, error)

	// Restore returns the value behind tok in session sid. ok is false when the
	// token is unknown in that session, which includes tokens from other sessions.
	Restore(ctx context.Context, sid, tok string) (value string, ok bool, err error)

	// Forget deletes a session and everything in it. The proxy calls it for
	// requests that sent no session header, whose tokens nobody can ever look
	// up again, so they do not sit in the vault until the TTL.
	Forget(ctx context.Context, sid string) error

	Close() error
}

// Item is one value to tokenize.
type Item struct {
	Kind  detect.Kind
	Value string
}

// Batcher is implemented by vaults that can allocate the tokens of a whole
// request in one round trip. A caller uses it only when Batching reports true;
// otherwise concurrent Tokenize calls are the better way to hide latency.
type Batcher interface {
	// TokenizeBatch returns one token per item, in order, with the same result
	// as calling Tokenize for each. Items need not be distinct.
	TokenizeBatch(ctx context.Context, sid string, items []Item) ([]string, error)

	// Batching reports whether TokenizeBatch is cheaper than concurrent
	// Tokenize calls on this vault as configured.
	Batching() bool
}

// valueIndex returns the lookup key for a value: a keyed hash, so the plaintext
// never appears as a map key or Redis field name, and so the index of a
// low-entropy value (SSN, phone) cannot be enumerated without the key.
func valueIndex(key []byte, kind detect.Kind, value string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(kind))
	m.Write([]byte{0})
	m.Write([]byte(value))
	return hex.EncodeToString(m.Sum(nil))
}
