package mask

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/vikasgowda3007/pseudonymbench/internal/synth"
	"github.com/vikasgowda3007/pseudonymbench/internal/vault"
)

// BenchmarkRequestRedis measures masking one request against a real Redis, so
// the cost of the vault round trips can be seen without the rest of the proxy.
// Set PSEUDONYMBENCH_TEST_REDIS (for example localhost:6379) to run it.
func BenchmarkRequestRedis(b *testing.B) {
	addr := os.Getenv("PSEUDONYMBENCH_TEST_REDIS")
	if addr == "" {
		b.Skip("set PSEUDONYMBENCH_TEST_REDIS to run against a real Redis")
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	g := synth.New(1)
	for _, payload := range []string{"pii", "pii-long"} {
		bodies := make([][]byte, 200)
		for i := range bodies {
			bodies[i], _ = g.ChatBody(payload, false)
		}
		for _, lua := range []bool{false, true} {
			sealer, _ := vault.NewSealer(vault.GenerateKey())
			prefix := fmt.Sprintf("pbbench%d", time.Now().UnixNano())
			v, err := vault.NewRedis(vault.RedisOptions{Client: client, Sealer: sealer, IndexKey: vault.GenerateKey(),
				TTL: time.Minute, Prefix: prefix, Lua: lua})
			if err != nil {
				b.Fatal(err)
			}
			var n atomic.Int64
			b.Run(fmt.Sprintf("%s/lua=%v", payload, lua), func(b *testing.B) {
				m := New(nil)
				for i := 0; i < b.N; i++ {
					sid := fmt.Sprintf("s%d", n.Add(1))
					sess := NewSession(v, sid)
					if _, _, err := m.Request(ctx, sess, bodies[i%len(bodies)]); err != nil {
						b.Fatal(err)
					}
					v.Forget(ctx, sid)
				}
			})
		}
	}
}
