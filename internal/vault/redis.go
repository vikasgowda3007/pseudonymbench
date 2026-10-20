package vault

import (
	"context"
	"errors"
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

func (r *Redis) tokenizeLua(ctx context.Context, sid string, kind detect.Kind, value, t2v, v2t, idx string) (string, error) {
	ttl := int(r.o.TTL.Seconds())
	if ttl < 1 {
		ttl = 1
	}
	for i := 0; i < maxAllocAttempts; i++ {
		tok := r.o.newToken(kind)
		res, err := allocScript.Run(ctx, r.o.Client, []string{t2v, v2t},
			idx, tok, r.o.Sealer.Seal(sid, tok, value), ttl, r.o.MaxPerSession).Slice()
		if err != nil {
			return "", err
		}
		if len(res) != 2 {
			return "", errors.New("vault: unexpected script reply")
		}
		code, _ := res[0].(int64)
		switch code {
		case 0, 1:
			got, _ := res[1].(string)
			return got, nil
		case 2:
			return "", ErrSessionFull
		}
		// 3: the candidate token exists in this session; try another
	}
	return "", errTokenSpace
}
