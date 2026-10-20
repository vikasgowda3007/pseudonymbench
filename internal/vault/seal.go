package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

var errTokenSpace = errors.New("vault: could not allocate an unused token")

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

// sealVersion prefixes every sealed value so the key can be rotated later.
const sealVersion byte = 1

// Sealer encrypts vault values with AES-256-GCM. The additional data binds a
// ciphertext to its session and token, so a value copied to another slot in
// Redis fails to open. The key lives in the proxy, never in Redis.
type Sealer struct{ gcm cipher.AEAD }

// NewSealer returns a Sealer for a 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("vault: key must be %d bytes, got %d", KeySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{gcm: gcm}, nil
}

// Seal returns version || nonce || ciphertext||tag.
func (s *Sealer) Seal(sid, tok, plaintext string) []byte {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic("vault: crypto/rand failed: " + err.Error())
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+s.gcm.Overhead())
	out = append(out, sealVersion)
	out = append(out, nonce...)
	return s.gcm.Seal(out, nonce, []byte(plaintext), aad(sid, tok))
}

// Open reverses Seal. It fails if the data, session or token do not match.
func (s *Sealer) Open(sid, tok string, sealed []byte) (string, error) {
	n := s.gcm.NonceSize()
	if len(sealed) < 1+n+s.gcm.Overhead() || sealed[0] != sealVersion {
		return "", errors.New("vault: malformed sealed value")
	}
	pt, err := s.gcm.Open(nil, sealed[1:1+n], sealed[1+n:], aad(sid, tok))
	if err != nil {
		return "", errors.New("vault: sealed value failed authentication")
	}
	return string(pt), nil
}

func aad(sid, tok string) []byte { return []byte(sid + "\x00" + tok) }

// ParseKey decodes a 32-byte key given as 64 hex characters or as base64.
func ParseKey(s string) ([]byte, error) {
	if b, err := hex.DecodeString(s); err == nil && len(b) == KeySize {
		return b, nil
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == KeySize {
			return b, nil
		}
	}
	return nil, fmt.Errorf("vault: key must be %d bytes as hex or base64", KeySize)
}

// GenerateKey returns a fresh random key.
func GenerateKey() []byte { return randomKey() }

func randomKey() []byte {
	k := make([]byte, KeySize)
	if _, err := rand.Read(k); err != nil {
		panic("vault: crypto/rand failed: " + err.Error())
	}
	return k
}

// Keys are the three independent keys the proxy derives from one master
// secret, so a single environment variable configures everything and no key
// is ever used for two purposes.
type Keys struct {
	Seal    []byte // AES-256-GCM key for vault values
	Index   []byte // HMAC key for the value index
	Session []byte // HMAC key that derives session ids from caller identity
}

// DeriveKeys expands master into purpose-specific keys with HMAC-SHA256.
func DeriveKeys(master []byte) Keys {
	d := func(label string) []byte {
		m := hmac.New(sha256.New, master)
		m.Write([]byte("pseudonymbench/" + label))
		return m.Sum(nil)
	}
	return Keys{Seal: d("seal"), Index: d("index"), Session: d("session")}
}
