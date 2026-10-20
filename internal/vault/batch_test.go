package vault

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
)

// roundTrips counts the network exchanges a client makes: one per single
// command, one per pipeline.
type roundTrips struct{ single, pipelines atomic.Int64 }

func (h *roundTrips) DialHook(next redis.DialHook) redis.DialHook { return next }
func (h *roundTrips) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error { h.single.Add(1); return next(ctx, cmd) }
}
func (h *roundTrips) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error { h.pipelines.Add(1); return next(ctx, cmds) }
}

func batchVault(t *testing.T, lua bool, cap int, nt func(detect.Kind) string) (*Redis, *miniredis.Miniredis, *roundTrips) {
	t.Helper()
	mr := miniredis.RunT(t)
	rt := &roundTrips{}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	client.AddHook(rt)
	sealer, err := NewSealer(GenerateKey())
	if err != nil {
		t.Fatal(err)
	}
	v, err := NewRedis(RedisOptions{Client: client, Sealer: sealer, IndexKey: GenerateKey(),
		TTL: time.Minute, MaxPerSession: cap, Lua: lua, newToken: nt})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	return v, mr, rt
}

func emails(n int) []Item {
	items := make([]Item, n)
	for i := range items {
		items[i] = Item{detect.Email, fmt.Sprintf("user%d@acme.com", i)}
	}
	return items
}

// A whole request's tokens cost one round trip, whatever the number of values.
func TestBatchIsOneRoundTrip(t *testing.T) {
	v, _, rt := batchVault(t, true, 1000, nil)
	if !v.Batching() {
		t.Fatal("Lua vault should batch")
	}
	// The first batch on a fresh server also loads the script, so measure the second.
	if _, err := v.TokenizeBatch(ctx, "warm", emails(3)); err != nil {
		t.Fatal(err)
	}
	rt.single.Store(0)
	rt.pipelines.Store(0)

	items := emails(40)
	toks, err := v.TokenizeBatch(ctx, "s1", items)
	if err != nil {
		t.Fatal(err)
	}
	if rt.pipelines.Load() != 1 || rt.single.Load() != 0 {
		t.Errorf("40 values took %d pipelines and %d single commands, want 1 and 0", rt.pipelines.Load(), rt.single.Load())
	}
	for i, tok := range toks {
		if got, ok, _ := v.Restore(ctx, "s1", tok); !ok || got != items[i].Value {
			t.Errorf("item %d: token %s restores %q (%v), want %q", i, tok, got, ok, items[i].Value)
		}
	}
}

// Batch and single allocation agree: same value, same token, in either order.
func TestBatchMatchesTokenize(t *testing.T) {
	v, _, _ := batchVault(t, true, 1000, nil)
	single, _ := v.Tokenize(ctx, "s1", detect.Email, "user1@acme.com")
	items := []Item{
		{detect.Email, "user0@acme.com"},
		{detect.Email, "user1@acme.com"}, // already allocated
		{detect.Email, "user0@acme.com"}, // repeated inside the batch
		{detect.Phone, "415-555-2671"},
		{detect.Email, "415-555-2671"}, // same text, other kind: its own token
	}
	toks, err := v.TokenizeBatch(ctx, "s1", items)
	if err != nil {
		t.Fatal(err)
	}
	if toks[1] != single {
		t.Errorf("existing value got %s in a batch, %s alone", toks[1], single)
	}
	if toks[0] != toks[2] {
		t.Errorf("a value repeated in one batch got %s and %s", toks[0], toks[2])
	}
	if toks[3] == toks[4] {
		t.Errorf("one token for two kinds: %s", toks[3])
	}
	for i, it := range items {
		if again, _ := v.Tokenize(ctx, "s1", it.Kind, it.Value); again != toks[i] {
			t.Errorf("item %d: Tokenize now says %s, the batch said %s", i, again, toks[i])
		}
	}
	// Sessions stay separate.
	other, _ := v.TokenizeBatch(ctx, "s2", items[:1])
	if other[0] == toks[0] {
		t.Error("two sessions share a token")
	}
	if _, ok, _ := v.Restore(ctx, "s2", toks[0]); ok {
		t.Error("token restored in another session")
	}
}

func TestBatchRespectsSessionCap(t *testing.T) {
	v, _, _ := batchVault(t, true, 5, nil)
	if _, err := v.TokenizeBatch(ctx, "s1", emails(5)); err != nil {
		t.Fatalf("5 values in a cap of 5: %v", err)
	}
	if _, err := v.TokenizeBatch(ctx, "s1", emails(5)); err != nil {
		t.Errorf("the same 5 values again: %v", err)
	}
	if _, err := v.TokenizeBatch(ctx, "s1", emails(6)); err != ErrSessionFull {
		t.Errorf("6 distinct values: err = %v, want ErrSessionFull", err)
	}
}

// A candidate token that collides with one in the session is replaced for that
// item only, and nothing is overwritten.
func TestBatchCollisionRetry(t *testing.T) {
	seq := []string{"<EMAIL_aaaaaaaa>", "<EMAIL_aaaaaaaa>", "<EMAIL_bbbbbbbb>", "<EMAIL_cccccccc>"}
	var n atomic.Int64
	v, _, _ := batchVault(t, true, 100, func(detect.Kind) string {
		return seq[min(int(n.Add(1))-1, len(seq)-1)]
	})
	// Load the script first: a fresh server answers NOSCRIPT, which would send
	// the pipeline twice and use up the scripted candidates.
	if err := allocScript.Load(ctx, v.o.Client).Err(); err != nil {
		t.Fatal(err)
	}
	items := []Item{{detect.Email, "a@acme.com"}, {detect.Email, "b@acme.com"}}
	toks, err := v.TokenizeBatch(ctx, "s1", items)
	if err != nil {
		t.Fatal(err)
	}
	if toks[0] != "<EMAIL_aaaaaaaa>" || toks[1] == toks[0] || toks[1] == "" {
		t.Fatalf("tokens = %v; the colliding candidate was reused", toks)
	}
	for i, tok := range toks {
		if got, ok, _ := v.Restore(ctx, "s1", tok); !ok || got != items[i].Value {
			t.Errorf("item %d restores %q (%v)", i, got, ok)
		}
	}
}

// Redis forgets scripts on restart or SCRIPT FLUSH; the batch loads it again.
func TestBatchSurvivesScriptFlush(t *testing.T) {
	v, _, _ := batchVault(t, true, 100, nil)
	if _, err := v.TokenizeBatch(ctx, "s1", emails(3)); err != nil {
		t.Fatal(err)
	}
	if err := v.o.Client.ScriptFlush(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	toks, err := v.TokenizeBatch(ctx, "s1", emails(6))
	if err != nil {
		t.Fatalf("after SCRIPT FLUSH: %v", err)
	}
	for i, tok := range toks {
		if got, ok, _ := v.Restore(ctx, "s1", tok); !ok || got != emails(6)[i].Value {
			t.Errorf("item %d restores %q (%v)", i, got, ok)
		}
	}
	// Three of the six existed already and kept their tokens.
	first, _ := v.TokenizeBatch(ctx, "s1", emails(3))
	for i := range first {
		if first[i] != toks[i] {
			t.Errorf("item %d changed token across the flush: %s -> %s", i, first[i], toks[i])
		}
	}
}

func TestBatchKeysExpire(t *testing.T) {
	v, mr, _ := batchVault(t, true, 100, nil)
	if _, err := v.TokenizeBatch(ctx, "s1", emails(4)); err != nil {
		t.Fatal(err)
	}
	for _, k := range mr.Keys() {
		if mr.TTL(k) <= 0 {
			t.Errorf("%s has no TTL", k)
		}
	}
	mr.FastForward(2 * time.Minute)
	if len(mr.Keys()) != 0 {
		t.Errorf("keys outlived the TTL: %v", mr.Keys())
	}
}

// Without Lua there is no single-round-trip form: Batching says so, and
// TokenizeBatch still returns correct tokens.
func TestBatchWithoutLua(t *testing.T) {
	v, _, _ := batchVault(t, false, 100, nil)
	if v.Batching() {
		t.Error("the HSETNX vault should not claim to batch")
	}
	items := emails(4)
	toks, err := v.TokenizeBatch(ctx, "s1", items)
	if err != nil {
		t.Fatal(err)
	}
	for i, tok := range toks {
		if got, ok, _ := v.Restore(ctx, "s1", tok); !ok || got != items[i].Value {
			t.Errorf("item %d restores %q (%v)", i, got, ok)
		}
	}
}

// More values than one pipeline carries are split, not refused.
func TestBatchLargerThanOnePipeline(t *testing.T) {
	v, _, rt := batchVault(t, true, 10000, nil)
	items := emails(maxBatch*2 + 10)
	toks, err := v.TokenizeBatch(ctx, "s1", items)
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != len(items) {
		t.Fatalf("got %d tokens for %d items", len(toks), len(items))
	}
	seen := map[string]bool{}
	for i, tok := range toks {
		if seen[tok] {
			t.Fatalf("token %s issued twice", tok)
		}
		seen[tok] = true
		if got, ok, _ := v.Restore(ctx, "s1", tok); !ok || got != items[i].Value {
			t.Fatalf("item %d restores %q (%v)", i, got, ok)
		}
	}
	if p := rt.pipelines.Load(); p < 3 {
		t.Errorf("%d pipelines for %d items, expected at least 3", p, len(items))
	}
}

func TestNoBatchKeepsPerValueAllocation(t *testing.T) {
	mr := miniredis.RunT(t)
	sealer, _ := NewSealer(GenerateKey())
	v, err := NewRedis(RedisOptions{Client: redis.NewClient(&redis.Options{Addr: mr.Addr()}), Sealer: sealer,
		IndexKey: GenerateKey(), Lua: true, NoBatch: true})
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	if v.Batching() {
		t.Error("NoBatch should turn Batching off")
	}
	// TokenizeBatch itself still works, one value at a time.
	if _, err := v.TokenizeBatch(ctx, "s1", emails(3)); err != nil {
		t.Fatal(err)
	}
}
