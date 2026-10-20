package vault

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
)

// These tests and benchmarks need a real Redis server, because miniredis is an
// emulation and a vault is only trustworthy against the real thing. Point
// PSEUDONYMBENCH_TEST_REDIS at one (for example localhost:6379) to run them:
//
//	docker run --rm -p 6379:6379 redis:7-alpine
//	PSEUDONYMBENCH_TEST_REDIS=localhost:6379 go test ./internal/vault -run Real -bench Redis
//
// They use a throwaway key prefix and delete what they create.

func realRedis(tb testing.TB) *redis.Client {
	tb.Helper()
	addr := os.Getenv("PSEUDONYMBENCH_TEST_REDIS")
	if addr == "" {
		tb.Skip("set PSEUDONYMBENCH_TEST_REDIS to run against a real Redis")
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	if err := c.Ping(ctx).Err(); err != nil {
		tb.Fatalf("cannot reach Redis at %s: %v", addr, err)
	}
	tb.Cleanup(func() { c.Close() })
	return c
}

var realPrefixes atomic.Int64

func realVault(tb testing.TB, lua bool, ttl time.Duration, cap int, nt func(detect.Kind) string) *Redis {
	tb.Helper()
	c := realRedis(tb)
	sealer, err := NewSealer(GenerateKey())
	if err != nil {
		tb.Fatal(err)
	}
	prefix := fmt.Sprintf("pbtest%d_%d", time.Now().UnixNano(), realPrefixes.Add(1))
	v, err := NewRedis(RedisOptions{Client: c, Sealer: sealer, IndexKey: GenerateKey(), TTL: ttl,
		Prefix: prefix, MaxPerSession: cap, Lua: lua, newToken: nt})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		keys, _ := c.Keys(ctx, prefix+":*").Result()
		if len(keys) > 0 {
			c.Del(ctx, keys...)
		}
	})
	return v
}

// The contract tests that matter most, run against a real server in both
// allocation modes: same value same token, isolation, concurrency, TTL, cap.
func TestRealRedisContract(t *testing.T) {
	for _, lua := range []bool{false, true} {
		t.Run(fmt.Sprintf("lua=%v", lua), func(t *testing.T) {
			v := realVault(t, lua, 30*time.Second, 5, nil)

			a, err := v.Tokenize(ctx, "s1", detect.Email, "john@acme.com")
			if err != nil {
				t.Fatal(err)
			}
			if again, _ := v.Tokenize(ctx, "s1", detect.Email, "john@acme.com"); again != a {
				t.Errorf("same value got %q then %q", a, again)
			}
			if got, ok, _ := v.Restore(ctx, "s1", a); !ok || got != "john@acme.com" {
				t.Errorf("Restore = %q, %v", got, ok)
			}
			other, _ := v.Tokenize(ctx, "s2", detect.Email, "john@acme.com")
			if other == a {
				t.Error("two sessions share a token")
			}
			if _, ok, _ := v.Restore(ctx, "s2", a); ok {
				t.Error("token restored in another session")
			}

			// Every key has a TTL.
			t2v, v2t := v.keys("s1")
			for _, k := range []string{t2v, v2t} {
				if ttl := v.o.Client.TTL(ctx, k).Val(); ttl <= 0 {
					t.Errorf("%s has TTL %v", k, ttl)
				}
			}
			// No plaintext in Redis.
			fields, _ := v.o.Client.HGetAll(ctx, t2v).Result()
			for f, val := range fields {
				if contains(f, "john") || contains(val, "john@acme.com") {
					t.Errorf("plaintext in Redis: %q => %q", f, val)
				}
			}

			// Concurrent callers agree.
			const n = 64
			toks := make([]string, n)
			done := make(chan int, n)
			for i := 0; i < n; i++ {
				go func(i int) {
					toks[i], _ = v.Tokenize(ctx, "s3", detect.Phone, "415-555-2671")
					done <- i
				}(i)
			}
			for i := 0; i < n; i++ {
				<-done
			}
			for _, tok := range toks {
				if tok != toks[0] || tok == "" {
					t.Fatalf("concurrent callers disagree: %q vs %q", tok, toks[0])
				}
			}
			if got, ok, _ := v.Restore(ctx, "s3", toks[0]); !ok || got != "415-555-2671" {
				t.Errorf("winner restores %q (%v)", got, ok)
			}

			// Cap counts distinct values, not racing orphans.
			for i := 0; i < 5; i++ {
				if _, err := v.Tokenize(ctx, "s4", detect.Email, fmt.Sprintf("u%d@acme.com", i)); err != nil {
					t.Fatalf("value %d: %v", i, err)
				}
			}
			if _, err := v.Tokenize(ctx, "s4", detect.Email, "extra@acme.com"); err != ErrSessionFull {
				t.Errorf("err = %v, want ErrSessionFull", err)
			}

			// Forget.
			if err := v.Forget(ctx, "s1"); err != nil {
				t.Fatal(err)
			}
			if _, ok, _ := v.Restore(ctx, "s1", a); ok {
				t.Error("token restored after Forget")
			}
		})
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// BenchmarkRedisTokenize compares the two allocation strategies on a real
// server. "new" allocates a fresh value every time (the expensive path: two
// to three round trips with HSETNX, one with Lua); "hit" looks up an existing
// value (one round trip either way). Run with -cpu to see contention.
func BenchmarkRedisTokenize(b *testing.B) {
	for _, mode := range []struct {
		name string
		lua  bool
	}{{"hsetnx", false}, {"lua", true}} {
		b.Run(mode.name+"/new", func(b *testing.B) {
			v := realVault(b, mode.lua, time.Minute, 1<<30, nil)
			var n atomic.Int64
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					i := n.Add(1)
					if _, err := v.Tokenize(ctx, fmt.Sprintf("s%d", i%64), detect.Email, fmt.Sprintf("user%d@acme.com", i)); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
		b.Run(mode.name+"/hit", func(b *testing.B) {
			v := realVault(b, mode.lua, time.Minute, 1<<30, nil)
			if _, err := v.Tokenize(ctx, "s", detect.Email, "john@acme.com"); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, err := v.Tokenize(ctx, "s", detect.Email, "john@acme.com"); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
