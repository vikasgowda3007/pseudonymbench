package main

import (
	"strings"
	"sync/atomic"
	"testing"
)

func testConfig() config {
	return config{chunks: 240, chunkBytes: 4, echo: true, overflow: &atomic.Int64{}}
}

// The whole point of the mock is that response timing does not depend on the
// request. Replies must be exactly the same length whatever is echoed.
func TestReplyLengthIsRequestIndependent(t *testing.T) {
	c := testConfig()
	bodies := []string{
		`{"messages":[{"role":"user","content":"hello"}]}`,
		`{"messages":[{"role":"user","content":"mail john@acme.com"}]}`,
		`{"messages":[{"role":"user","content":"mail <EMAIL_7f3a9c21>"}]}`,
		`{"messages":[{"role":"user","content":"a@b.co c@d.co e@f.co g@h.co 415-555-2671 078-05-1120"}]}`,
		`{"messages":[{"role":"user","content":"x"},{"role":"tool","content":"4111 1111 1111 1111"}]}`,
		`not json at all`,
	}
	for _, b := range bodies {
		got := c.reply([]byte(b))
		if len(got) != 960 {
			t.Errorf("body %q: reply is %d bytes, want 960", b, len(got))
		}
		if n := len(splitBytes(got, 4)); n != 240 {
			t.Errorf("body %q: %d chunks, want 240", b, n)
		}
	}
	if c.overflow.Load() != 0 {
		t.Errorf("overflow = %d for replies that fit", c.overflow.Load())
	}
}

func TestReplyEchoesPIIAndTokens(t *testing.T) {
	c := testConfig()
	direct := c.reply([]byte(`{"messages":[{"role":"user","content":"mail john@acme.com"}]}`))
	proxied := c.reply([]byte(`{"messages":[{"role":"user","content":"mail <EMAIL_7f3a9c21>"}]}`))
	if !strings.Contains(direct, "john@acme.com") || !strings.Contains(proxied, "<EMAIL_7f3a9c21>") {
		t.Errorf("echo missing:\n%s\n%s", direct, proxied)
	}
}

func TestOverflowIsCounted(t *testing.T) {
	c := testConfig()
	c.chunks = 4 // 16-byte budget
	got := c.reply([]byte(`{"messages":[{"role":"user","content":"john@acme.com jane@acme.com"}]}`))
	if len(got) <= 16 || c.overflow.Load() != 1 {
		t.Errorf("len %d, overflow %d", len(got), c.overflow.Load())
	}
}
