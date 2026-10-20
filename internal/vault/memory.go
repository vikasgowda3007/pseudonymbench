package vault

import (
	"context"
	"sync"
	"time"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
	"github.com/vikasgowda3007/pseudonymbench/internal/token"
)

// MemoryOptions configures a Memory vault. Zero values pick sane defaults.
type MemoryOptions struct {
	TTL           time.Duration // session lifetime, refreshed on Tokenize; default 15m
	MaxPerSession int           // distinct values per session; default 10000
	IndexKey      []byte        // HMAC key for the value index; random if empty

	now      func() time.Time
	newToken func(detect.Kind) string
}

// Memory is an in-process vault. Values live in process memory, which is the
// trust boundary of the proxy anyway. Use it for single-instance deployments
// and as the baseline row in benchmarks.
type Memory struct {
	opts MemoryOptions

	mu        sync.Mutex
	sessions  map[string]*memSession
	lastSweep time.Time
}

type memSession struct {
	t2v map[string]string // token -> value
	v2t map[string]string // value index -> token
	exp time.Time
}

// NewMemory returns an empty in-memory vault.
func NewMemory(o MemoryOptions) *Memory {
	if o.TTL <= 0 {
		o.TTL = 15 * time.Minute
	}
	if o.MaxPerSession <= 0 {
		o.MaxPerSession = 10000
	}
	if len(o.IndexKey) == 0 {
		o.IndexKey = randomKey()
	}
	if o.now == nil {
		o.now = time.Now
	}
	if o.newToken == nil {
		o.newToken = token.New
	}
	return &Memory{opts: o, sessions: map[string]*memSession{}, lastSweep: o.now()}
}

func (m *Memory) Tokenize(_ context.Context, sid string, kind detect.Kind, value string) (string, error) {
	idx := valueIndex(m.opts.IndexKey, kind, value)

	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.opts.now()
	m.sweep(now)

	s := m.sessions[sid]
	if s == nil || now.After(s.exp) {
		s = &memSession{t2v: map[string]string{}, v2t: map[string]string{}}
		m.sessions[sid] = s
	}
	s.exp = now.Add(m.opts.TTL)

	if tok, ok := s.v2t[idx]; ok {
		return tok, nil
	}
	if len(s.t2v) >= m.opts.MaxPerSession {
		return "", ErrSessionFull
	}
	for i := 0; i < maxAllocAttempts; i++ {
		tok := m.opts.newToken(kind)
		if _, taken := s.t2v[tok]; taken {
			continue
		}
		s.t2v[tok] = value
		s.v2t[idx] = tok
		return tok, nil
	}
	return "", errTokenSpace
}

func (m *Memory) Restore(_ context.Context, sid, tok string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.opts.now()
	s := m.sessions[sid]
	if s == nil || now.After(s.exp) {
		return "", false, nil
	}
	v, ok := s.t2v[tok]
	return v, ok, nil
}

func (m *Memory) Forget(_ context.Context, sid string) error {
	m.mu.Lock()
	delete(m.sessions, sid)
	m.mu.Unlock()
	return nil
}

func (m *Memory) Close() error { return nil }

// sweep drops expired sessions, at most once per half TTL. Caller holds mu.
func (m *Memory) sweep(now time.Time) {
	if now.Sub(m.lastSweep) < m.opts.TTL/2 {
		return
	}
	for id, s := range m.sessions {
		if now.After(s.exp) {
			delete(m.sessions, id)
		}
	}
	m.lastSweep = now
}

// Sessions returns the number of live sessions. Used by tests and metrics.
func (m *Memory) Sessions() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}
