package vault

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/token"
)

var ctx = context.Background()

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// backend builds a vault with a short TTL and a hook to move time forward.
type backend struct {
	name    string
	make    func(t *testing.T, ttl time.Duration, newToken func(detect.Kind) string) (v Vault, advance func(time.Duration))
	inspect func(t *testing.T) string // dumps stored state, "" if not applicable
}

func memoryBackend() backend {
	return backend{
		name: "memory",
		make: func(t *testing.T, ttl time.Duration, nt func(detect.Kind) string) (Vault, func(time.Duration)) {
			c := &clock{t: time.Unix(1_700_000_000, 0)}
			return NewMemory(MemoryOptions{TTL: ttl, MaxPerSession: 5, now: c.now, newToken: nt}), c.advance
		},
	}
}

func redisBackend() backend {
	var mr *miniredis.Miniredis
	return backend{
		name: "redis",
		make: func(t *testing.T, ttl time.Duration, nt func(detect.Kind) string) (Vault, func(time.Duration)) {
			mr = miniredis.RunT(t)
			sealer, err := NewSealer(GenerateKey())
			if err != nil {
				t.Fatal(err)
			}
			v, err := NewRedis(RedisOptions{
				Client: redis.NewClient(&redis.Options{Addr: mr.Addr()}), Sealer: sealer,
				IndexKey: GenerateKey(), TTL: ttl, MaxPerSession: 5, newToken: nt,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { v.Close() })
			return v, mr.FastForward
		},
		inspect: func(t *testing.T) string {
			var b strings.Builder
			for _, k := range mr.Keys() {
				b.WriteString(k + "\n")
				if fields, err := mr.HKeys(k); err == nil {
					for _, f := range fields {
						val := mr.HGet(k, f)
						b.WriteString("  " + f + " = " + val + "\n")
					}
				}
			}
			return b.String()
		},
	}
}

func eachBackend(t *testing.T, f func(t *testing.T, b backend)) {
	for _, b := range []backend{memoryBackend(), redisBackend()} {
		t.Run(b.name, func(t *testing.T) { f(t, b) })
	}
}

func TestSameValueSameToken(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		v, _ := b.make(t, time.Minute, nil)
		a, err := v.Tokenize(ctx, "s1", detect.Email, "john@acme.com")
		if err != nil || !token.Valid(a) || token.Kind(a) != detect.Email {
			t.Fatalf("Tokenize = %q, %v", a, err)
		}
		again, _ := v.Tokenize(ctx, "s1", detect.Email, "john@acme.com")
		other, _ := v.Tokenize(ctx, "s1", detect.Email, "jane@acme.com")
		if again != a {
			t.Errorf("same value got %q then %q", a, again)
		}
		if other == a {
			t.Errorf("different values share token %q", a)
		}
	})
}

func TestRestore(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		v, _ := b.make(t, time.Minute, nil)
		tok, _ := v.Tokenize(ctx, "s1", detect.Card, "4111 1111 1111 1111")
		got, ok, err := v.Restore(ctx, "s1", tok)
		if err != nil || !ok || got != "4111 1111 1111 1111" {
			t.Fatalf("Restore = %q, %v, %v", got, ok, err)
		}
		if _, ok, _ := v.Restore(ctx, "s1", "<CARD_00000000>"); ok {
			t.Error("unknown token restored")
		}
	})
}

func TestSessionsAreIsolated(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		v, _ := b.make(t, time.Minute, nil)
		tok1, _ := v.Tokenize(ctx, "s1", detect.SSN, "078-05-1120")
		tok2, _ := v.Tokenize(ctx, "s2", detect.SSN, "078-05-1120")
		if tok1 == tok2 {
			t.Error("two sessions got the same token for the same value")
		}
		if _, ok, _ := v.Restore(ctx, "s2", tok1); ok {
			t.Error("token from session s1 restored in session s2")
		}
	})
}

func TestExpiry(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		v, advance := b.make(t, time.Minute, nil)
		tok, _ := v.Tokenize(ctx, "s1", detect.Phone, "415-555-2671")
		advance(30 * time.Second)
		// Tokenize refreshes the TTL, so the session survives past the first minute.
		v.Tokenize(ctx, "s1", detect.Phone, "415-555-2671")
		advance(45 * time.Second)
		if _, ok, _ := v.Restore(ctx, "s1", tok); !ok {
			t.Error("session expired despite a refresh")
		}
		advance(2 * time.Minute)
		if _, ok, _ := v.Restore(ctx, "s1", tok); ok {
			t.Error("token restored after TTL")
		}
	})
}

func TestSessionCap(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		v, _ := b.make(t, time.Minute, nil)
		for i := 0; i < 5; i++ {
			if _, err := v.Tokenize(ctx, "s1", detect.Email, "u"+string(rune('a'+i))+"@acme.com"); err != nil {
				t.Fatalf("value %d: %v", i, err)
			}
		}
		if _, err := v.Tokenize(ctx, "s1", detect.Email, "extra@acme.com"); !errors.Is(err, ErrSessionFull) {
			t.Fatalf("err = %v, want ErrSessionFull", err)
		}
		// Known values still resolve when the session is full.
		if _, err := v.Tokenize(ctx, "s1", detect.Email, "ua@acme.com"); err != nil {
			t.Errorf("existing value failed on a full session: %v", err)
		}
	})
}

func TestCollisionRetry(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		calls := 0
		seq := []string{"<EMAIL_aaaaaaaa>", "<EMAIL_aaaaaaaa>", "<EMAIL_bbbbbbbb>"}
		v, _ := b.make(t, time.Minute, func(detect.Kind) string {
			s := seq[min(calls, len(seq)-1)]
			calls++
			return s
		})
		first, _ := v.Tokenize(ctx, "s1", detect.Email, "a@acme.com")
		second, err := v.Tokenize(ctx, "s1", detect.Email, "b@acme.com")
		if err != nil {
			t.Fatal(err)
		}
		if first != "<EMAIL_aaaaaaaa>" || second != "<EMAIL_bbbbbbbb>" {
			t.Errorf("tokens = %q, %q; colliding token was reused", first, second)
		}
		if got, ok, _ := v.Restore(ctx, "s1", first); !ok || got != "a@acme.com" {
			t.Errorf("first token now restores %q (%v)", got, ok)
		}
	})
}

func TestConcurrentSameValue(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		v, _ := b.make(t, time.Minute, nil)
		const n = 32
		toks := make([]string, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				var err error
				toks[i], err = v.Tokenize(ctx, "s1", detect.Email, "race@acme.com")
				if err != nil {
					t.Errorf("caller %d: %v", i, err)
				}
			}(i)
		}
		wg.Wait()
		for _, tok := range toks {
			if tok != toks[0] || tok == "" {
				t.Fatalf("concurrent callers disagree: %q vs %q", tok, toks[0])
			}
		}
		if got, ok, _ := v.Restore(ctx, "s1", toks[0]); !ok || got != "race@acme.com" {
			t.Errorf("winner token restores %q (%v)", got, ok)
		}
	})
}

func TestRedisStoresNoPlaintext(t *testing.T) {
	b := redisBackend()
	v, _ := b.make(t, time.Minute, nil)
	secrets := []string{"john@acme.com", "078-05-1120"}
	v.Tokenize(ctx, "s1", detect.Email, secrets[0])
	v.Tokenize(ctx, "s1", detect.SSN, secrets[1])
	dump := b.inspect(t)
	for _, s := range secrets {
		if strings.Contains(dump, s) || strings.Contains(dump, strings.ReplaceAll(s, "-", "")) {
			t.Fatalf("Redis holds plaintext %q:\n%s", s, dump)
		}
	}
	if !strings.Contains(dump, "{s1}") {
		t.Errorf("keys lack the session hash tag:\n%s", dump)
	}
}

func TestSealer(t *testing.T) {
	key := GenerateKey()
	s, err := NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed := s.Seal("s1", "<EMAIL_aaaaaaaa>", "john@acme.com")
	if bytes.Contains(sealed, []byte("john")) {
		t.Fatal("ciphertext contains plaintext")
	}
	if got, err := s.Open("s1", "<EMAIL_aaaaaaaa>", sealed); err != nil || got != "john@acme.com" {
		t.Fatalf("Open = %q, %v", got, err)
	}
	// Bound to session and token: a copied entry must not open elsewhere.
	if _, err := s.Open("s2", "<EMAIL_aaaaaaaa>", sealed); err == nil {
		t.Error("opened under another session")
	}
	if _, err := s.Open("s1", "<EMAIL_bbbbbbbb>", sealed); err == nil {
		t.Error("opened under another token")
	}
	// Bit flip anywhere must fail authentication.
	for i := range sealed {
		bad := append([]byte(nil), sealed...)
		bad[i] ^= 1
		if _, err := s.Open("s1", "<EMAIL_aaaaaaaa>", bad); err == nil {
			t.Fatalf("tampered byte %d accepted", i)
		}
	}
	if other := s.Seal("s1", "<EMAIL_aaaaaaaa>", "john@acme.com"); bytes.Equal(other, sealed) {
		t.Error("two seals of the same value are identical: nonce reuse")
	}
	if _, err := NewSealer(key[:16]); err == nil {
		t.Error("short key accepted")
	}
}

func TestParseKey(t *testing.T) {
	k := GenerateKey()
	hexKey := ""
	for _, b := range k {
		hexKey += string("0123456789abcdef"[b>>4]) + string("0123456789abcdef"[b&15])
	}
	got, err := ParseKey(hexKey)
	if err != nil || !bytes.Equal(got, k) {
		t.Fatalf("hex: %v", err)
	}
	if _, err := ParseKey("short"); err == nil {
		t.Error("garbage key accepted")
	}
}
