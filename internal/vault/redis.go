package vault

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/token"
)

// RedisOptions configures a Redis vault.
type RedisOptions struct {
	Client   redis.UniversalClient
	Sealer   *Sealer       // encrypts values before they reach Redis
	IndexKey []byte        // HMAC key for the value index
	TTL      time.Duration // session lifetime, refreshed on Tokenize; default 15m
	Prefix   string        // key namespace; default "pb"

	MaxPerSession int // distinct values per session; default 10000

	// Lua allocates tokens with one server-side script instead of the
	// HSETNX sequence. It saves round trips on a new value (one instead of
	// two to three) at the price of running code inside the Redis server.
	// Both modes are tested against the same contract; pick by measurement.
	Lua bool

	// NoBatch turns off the pipelined TokenizeBatch even with Lua on, so a
	// request's tokens are allocated by concurrent single calls instead. It
	// exists so the two can be measured against each other.
	NoBatch bool

	newToken func(detect.Kind) string
}

// Redis keeps sessions in Redis. Each session is two hashes sharing a hash tag
// so they live in one cluster slot and expire together:
//
//	{prefix}:{sid}:t2v   token       -> AES-GCM sealed value
//	{prefix}:{sid}:v2t   value index -> token
//
// Allocation needs no Lua: HSETNX on t2v claims a token, HSETNX on v2t decides
// the race between concurrent requests for the same value, and the loser
// removes its orphan token.
type Redis struct{ o RedisOptions }

// NewRedis returns a Redis-backed vault.
func NewRedis(o RedisOptions) (*Redis, error) {
	if o.Client == nil || o.Sealer == nil || len(o.IndexKey) == 0 {
		return nil, errors.New("vault: Redis needs a client, a sealer and an index key")
	}
	if o.TTL <= 0 {
		o.TTL = 15 * time.Minute
	}
	if o.Prefix == "" {
		o.Prefix = "pb"
	}
	if o.MaxPerSession <= 0 {
		o.MaxPerSession = 10000
	}
	if o.newToken == nil {
		o.newToken = token.New
	}
	return &Redis{o: o}, nil
}

func (r *Redis) keys(sid string) (t2v, v2t string) {
	base := r.o.Prefix + ":{" + sid + "}:"
	return base + "t2v", base + "v2t"
}

func (r *Redis) Tokenize(ctx context.Context, sid string, kind detect.Kind, value string) (string, error) {
	t2v, v2t := r.keys(sid)
	idx := valueIndex(r.o.IndexKey, kind, value)
	if r.o.Lua {
		return r.tokenizeLua(ctx, sid, kind, value, t2v, v2t, idx)
	}

	// One round trip for the common case: look up and refresh the TTL.
	pipe := r.o.Client.Pipeline()
	get := pipe.HGet(ctx, v2t, idx)
	size := pipe.HLen(ctx, v2t) // distinct values; orphan tokens from races don't count
	pipe.Expire(ctx, t2v, r.o.TTL)
	pipe.Expire(ctx, v2t, r.o.TTL)
	_, err := pipe.Exec(ctx)
	if err != nil && !errors.Is(err, redis.Nil) {
		return "", err
	}
	if tok, gerr := get.Result(); gerr == nil {
		return tok, nil
	} else if !errors.Is(gerr, redis.Nil) {
		return "", gerr
	}

	if n, _ := size.Result(); n >= int64(r.o.MaxPerSession) {
		return "", ErrSessionFull
	}

	for i := 0; i < maxAllocAttempts; i++ {
		tok := r.o.newToken(kind)
		// Claim the token and give the session an expiry in one transaction, so
		// a failure between the two cannot leave sealed values with no TTL.
		tx := r.o.Client.TxPipeline()
		claim := tx.HSetNX(ctx, t2v, tok, r.o.Sealer.Seal(sid, tok, value))
		tx.Expire(ctx, t2v, r.o.TTL)
		if _, err := tx.Exec(ctx); err != nil {
			return "", err
		}
		if claimed, _ := claim.Result(); !claimed {
			continue // token collision: pick another
		}
		tx = r.o.Client.TxPipeline()
		won := tx.HSetNX(ctx, v2t, idx, tok)
		tx.Expire(ctx, v2t, r.o.TTL)
		tx.Expire(ctx, t2v, r.o.TTL)
		if _, err := tx.Exec(ctx); err != nil {
			return "", err
		}
		if w, _ := won.Result(); w {
			return tok, nil
		}
		// A concurrent request registered this value first: adopt its token.
		winner, err := r.o.Client.HGet(ctx, v2t, idx).Result()
		if err != nil {
			return "", err
		}
		r.o.Client.HDel(ctx, t2v, tok)
		return winner, nil
	}
	return "", errTokenSpace
}

func (r *Redis) Restore(ctx context.Context, sid, tok string) (string, bool, error) {
	t2v, _ := r.keys(sid)
	raw, err := r.o.Client.HGet(ctx, t2v, tok).Bytes()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	v, err := r.o.Sealer.Open(sid, tok, raw)
	if err != nil {
		return "", false, nil // tampered or foreign entry: treat as unknown
	}
	return v, true, nil
}

func (r *Redis) Forget(ctx context.Context, sid string) error {
	t2v, v2t := r.keys(sid)
	return r.o.Client.Del(ctx, t2v, v2t).Err()
}

// Remote reports that every call is a network round trip.
func (r *Redis) Remote() bool { return true }

func (r *Redis) Close() error { return r.o.Client.Close() }

// allocScript is get-or-create for a (session, value) pair, run atomically by
// the server. KEYS: t2v, v2t. ARGV: value index, candidate token, sealed value,
// TTL in seconds, session cap. It returns {status, token}: 0 created, 1 existing,
// 2 session full, 3 candidate token already in use.
var allocScript = redis.NewScript(`
local existing = redis.call('HGET', KEYS[2], ARGV[1])
if existing then
  redis.call('EXPIRE', KEYS[1], ARGV[4])
  redis.call('EXPIRE', KEYS[2], ARGV[4])
  return {1, existing}
end
if redis.call('HLEN', KEYS[2]) >= tonumber(ARGV[5]) then
  return {2, ''}
end
if redis.call('HEXISTS', KEYS[1], ARGV[2]) == 1 then
  return {3, ''}
end
redis.call('HSET', KEYS[1], ARGV[2], ARGV[3])
redis.call('HSET', KEYS[2], ARGV[1], ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[4])
redis.call('EXPIRE', KEYS[2], ARGV[4])
return {0, ARGV[2]}
`)

func (r *Redis) ttlSeconds() int {
	return max(int(r.o.TTL.Seconds()), 1)
}

func (r *Redis) tokenizeLua(ctx context.Context, sid string, kind detect.Kind, value, t2v, v2t, idx string) (string, error) {
	ttl := r.ttlSeconds()
	for i := 0; i < maxAllocAttempts; i++ {
		tok := r.o.newToken(kind)
		res, err := allocScript.Run(ctx, r.o.Client, []string{t2v, v2t},
			idx, tok, r.o.Sealer.Seal(sid, tok, value), ttl, r.o.MaxPerSession).Slice()
		if err != nil {
			return "", err
		}
		got, code, err := scriptResult(res)
		if err != nil {
			return "", err
		}
		switch code {
		case 0, 1:
			return got, nil
		case 2:
			return "", ErrSessionFull
		}
		// 3: the candidate token exists in this session; try another
	}
	return "", errTokenSpace
}

// scriptResult unpacks the {status, token} reply of allocScript.
func scriptResult(res []any) (tok string, code int64, err error) {
	if len(res) != 2 {
		return "", 0, errors.New("vault: unexpected script reply")
	}
	code, _ = res[0].(int64)
	tok, _ = res[1].(string)
	return tok, code, nil
}

// maxBatch bounds how many scripts go into one pipeline, so a request with a
// very large number of values does not build an unbounded command buffer.
const maxBatch = 256

// Batching reports that TokenizeBatch pipelines the allocation scripts. The
// HSETNX sequence is several dependent round trips per value and cannot be
// folded into one, so it keeps the per-value path (as does NoBatch).
func (r *Redis) Batching() bool { return r.o.Lua && !r.o.NoBatch }

// TokenizeBatch allocates the tokens of a whole request. With Lua enabled, the
// allocation scripts for up to maxBatch values go to the server in one pipelined
// round trip; each script is still atomic, and they run in order, so a value
// that appears twice in a batch gets one token.
func (r *Redis) TokenizeBatch(ctx context.Context, sid string, items []Item) ([]string, error) {
	out := make([]string, len(items))
	if !r.Batching() {
		for i, it := range items {
			tok, err := r.Tokenize(ctx, sid, it.Kind, it.Value)
			if err != nil {
				return nil, err
			}
			out[i] = tok
		}
		return out, nil
	}
	for start := 0; start < len(items); start += maxBatch {
		end := min(start+maxBatch, len(items))
		if err := r.pipelineLua(ctx, sid, items[start:end], out[start:end]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *Redis) pipelineLua(ctx context.Context, sid string, items []Item, out []string) error {
	t2v, v2t := r.keys(sid)
	ttl := r.ttlSeconds()
	idx := make([]string, len(items))
	// A script cache that was flushed (restart, failover, SCRIPT FLUSH) answers
	// NOSCRIPT to EVALSHA; load the script and send the pipeline again. Scripts
	// that already ran find their values and return the existing tokens.
	for attempt := 0; ; attempt++ {
		pipe := r.o.Client.Pipeline()
		cmds := make([]*redis.Cmd, len(items))
		for i, it := range items {
			idx[i] = valueIndex(r.o.IndexKey, it.Kind, it.Value)
			tok := r.o.newToken(it.Kind)
			cmds[i] = allocScript.EvalSha(ctx, pipe, []string{t2v, v2t},
				idx[i], tok, r.o.Sealer.Seal(sid, tok, it.Value), ttl, r.o.MaxPerSession)
		}
		pipe.Exec(ctx) // each command's error is read below
		noScript := false
		for _, c := range cmds {
			if err := c.Err(); err != nil {
				if !strings.HasPrefix(err.Error(), "NOSCRIPT") {
					return err
				}
				noScript = true
			}
		}
		if noScript {
			if attempt > 0 {
				return errors.New("vault: Redis does not keep the allocation script")
			}
			if err := allocScript.Load(ctx, r.o.Client).Err(); err != nil {
				return err
			}
			continue
		}
		for i, c := range cmds {
			res, err := c.Slice()
			if err != nil {
				return err
			}
			tok, code, err := scriptResult(res)
			if err != nil {
				return err
			}
			switch code {
			case 0, 1:
				out[i] = tok
			case 2:
				return ErrSessionFull
			default: // 3: candidate token already in the session; allocate this one alone
				tok, err := r.tokenizeLua(ctx, sid, items[i].Kind, items[i].Value, t2v, v2t, idx[i])
				if err != nil {
					return err
				}
				out[i] = tok
			}
		}
		return nil
	}
}
