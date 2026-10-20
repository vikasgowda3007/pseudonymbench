// Package stream restores tokens in a streamed chat-completions response
// (server-sent events) without waiting for the stream to finish.
//
// A token such as <EMAIL_7f3a9c21> can arrive split across any number of
// chunks. The rewriter keeps one small state per (choice, field) and holds
// back only a trailing piece of text that could still grow into a token.
// Ordinary prose is forwarded in the same event it arrived in.
package stream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/vikasgowda3007/pseudonymbench/internal/jsonx"
	"github.com/vikasgowda3007/pseudonymbench/internal/mask"
	"github.com/vikasgowda3007/pseudonymbench/internal/token"
)

// ErrEventTooLarge is returned when one SSE event exceeds Options.MaxEventBytes.
var ErrEventTooLarge = errors.New("stream: SSE event too large")

// Options configures a Rewriter.
type Options struct {
	// AllowURLs restores tokens that appear inside URLs. Off by default: see
	// mask.Restorer.
	AllowURLs bool
	// MaxEventBytes bounds one SSE event. Default 4 MiB.
	MaxEventBytes int
}

// Stats summarises a finished stream. It holds counts only.
type Stats struct {
	mask.RestoreStats
	Events      int // SSE events read
	Rewritten   int // events re-serialized because text changed
	Passthrough int // events forwarded byte for byte
	Held        int // pushes that held back a possible token prefix
}

// Rewriter is an io.ReadCloser: reading it yields the upstream stream with
// tokens restored. It is not safe for concurrent use.
type Rewriter struct {
	ctx  context.Context
	src  *bufio.Reader
	body io.Closer
	sess *mask.Session
	opts Options

	chans map[chanKey]*channel
	meta  map[string]any // id, model, created ... copied onto synthetic chunks
	out   bytes.Buffer
	err   error
	stats Stats
}

type chanKind uint8

const (
	chContent chanKind = iota
	chRefusal
	chTranscript
	chArgs   // tool_calls[i].function.arguments
	chFnArgs // legacy function_call.arguments
)

type chanKey struct {
	choice int
	kind   chanKind
	tool   int
}

type channel struct {
	r    *mask.Restorer
	held string
}

// New returns a Rewriter reading from upstream. Closing the Rewriter closes
// upstream.
func New(ctx context.Context, upstream io.ReadCloser, sess *mask.Session, o Options) *Rewriter {
	if o.MaxEventBytes <= 0 {
		o.MaxEventBytes = 4 << 20
	}
	return &Rewriter{
		ctx: ctx, src: bufio.NewReaderSize(upstream, 32<<10), body: upstream, sess: sess, opts: o,
		chans: map[chanKey]*channel{}, meta: map[string]any{},
	}
}

func (r *Rewriter) Close() error { return r.body.Close() }

// Stats returns the counters so far; call it after the stream ended.
func (r *Rewriter) Stats() Stats {
	s := r.stats
	s.RestoreStats = mask.RestoreStats{}
	for _, ch := range r.chans {
		s.RestoreStats.Add(ch.r.Stats)
	}
	return s
}

func (r *Rewriter) Read(p []byte) (int, error) {
	for {
		if r.out.Len() > 0 {
			return r.out.Read(p)
		}
		if r.err != nil {
			return 0, r.err
		}
		r.fill()
	}
}

// fill reads one upstream event and appends the rewritten event to out.
func (r *Rewriter) fill() {
	lines, err := r.readEvent()
	if len(lines) > 0 {
		r.stats.Events++
		r.event(lines)
	}
	if err != nil {
		r.flushAll() // EOF without [DONE], or a broken connection: don't lose held text
		r.err = err
	}
}

// readLine reads one line, refusing to buffer more than budget bytes. A plain
// ReadBytes would hold an unbounded line in memory before any check could run.
func (r *Rewriter) readLine(budget int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.src.ReadSlice('\n')
		if len(line)+len(chunk) > budget {
			return nil, ErrEventTooLarge
		}
		line = append(line, chunk...)
		if err == bufio.ErrBufferFull {
			continue
		}
		return line, err
	}
}

// readEvent reads lines up to the blank line that ends an event. An oversize
// event returns ErrEventTooLarge and no lines: half an event is never forwarded.
func (r *Rewriter) readEvent() ([][]byte, error) {
	var lines [][]byte
	total := 0
	for {
		line, err := r.readLine(r.opts.MaxEventBytes - total)
		if errors.Is(err, ErrEventTooLarge) {
			return nil, err
		}
		total += len(line)
		if len(line) > 0 {
			lines = append(lines, line)
		}
		if err != nil {
			return lines, err
		}
		if isBlank(line) {
			return lines, nil
		}
	}
}

func isBlank(line []byte) bool {
	return len(line) == 1 && line[0] == '\n' || len(line) == 2 && line[0] == '\r' && line[1] == '\n'
}

var (
	dataPrefix = []byte("data:")
	doneMark   = []byte("[DONE]")
)

// event processes one SSE event, given as its raw lines.
func (r *Rewriter) event(lines [][]byte) {
	var payload [][]byte
	firstData := -1
	for i, l := range lines {
		if bytes.HasPrefix(l, dataPrefix) {
			if firstData < 0 {
				firstData = i
			}
			payload = append(payload, trimData(l))
		}
	}
	if firstData < 0 { // comment, ping or other non-data event
		r.passthrough(lines)
		return
	}
	data := bytes.Join(payload, []byte("\n"))
	if bytes.Equal(bytes.TrimSpace(data), doneMark) {
		r.flushAll()
		r.passthrough(lines)
		return
	}
	v, err := jsonx.Decode(data)
	chunk, ok := v.(map[string]any)
	if err != nil || !ok {
		r.passthrough(lines)
		return
	}
	if _, isErr := chunk["error"]; isErr {
		r.flushAll() // held text first, then the error frame
		r.passthrough(lines)
		return
	}
	if !r.rewriteChunk(chunk) {
		r.passthrough(lines)
		return
	}
	enc, err := jsonx.Encode(chunk)
	if err != nil {
		r.passthrough(lines)
		return
	}
	r.stats.Rewritten++
	for i, l := range lines {
		switch {
		case i == firstData:
			r.out.WriteString("data: ")
			r.out.Write(enc)
			r.out.WriteByte('\n')
		case bytes.HasPrefix(l, dataPrefix):
			// Extra data lines were merged into the first.
		case isBlank(l):
			r.out.WriteByte('\n')
		default:
			r.out.Write(l)
		}
	}
}

func (r *Rewriter) passthrough(lines [][]byte) {
	r.stats.Passthrough++
	for _, l := range lines {
		r.out.Write(l)
	}
}

func trimData(line []byte) []byte {
	d := bytes.TrimRight(line[len(dataPrefix):], "\r\n")
	if len(d) > 0 && d[0] == ' ' {
		d = d[1:]
	}
	return d
}

func (r *Rewriter) anyHeld() bool {
	for _, ch := range r.chans {
		if ch.held != "" {
			return true
		}
	}
	return false
}

func (r *Rewriter) channel(k chanKey) *channel {
	ch := r.chans[k]
	if ch == nil {
		ch = &channel{r: &mask.Restorer{Session: r.sess, AllowURLs: r.opts.AllowURLs}}
		r.chans[k] = ch
	}
	return ch
}

// push feeds a piece of text into a channel and returns what is safe to emit.
func (r *Rewriter) push(k chanKey, s string) string {
	ch := r.channel(k)
	text := ch.held + s
	partial := ""
	if k.kind == chArgs || k.kind == chFnArgs {
		// Arguments are JSON text, and some backends escape "<" and ">" as
		// unicode escapes inside it. Decode just those two so a token written
		// that way is still recognised (see decodeBracketEscapes).
		text, partial = decodeBracketEscapes(text)
	}
	safe, held := token.SplitTail(text)
	if held != "" || partial != "" {
		r.stats.Held++
	}
	ch.held = held + partial
	return ch.r.Text(r.ctx, safe)
}

// rewriteChunk restores tokens in the text fields of one chunk, in place, and
// reports whether anything changed.
func (r *Rewriter) rewriteChunk(chunk map[string]any) bool {
	for _, k := range []string{"id", "object", "created", "model", "system_fingerprint", "service_tier"} {
		if v, ok := chunk[k]; ok {
			r.meta[k] = v
		}
	}
	choices, _ := chunk["choices"].([]any)
	changed := false
	for pos, c := range choices {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		idx := intOf(cm["index"], pos)
		if delta, ok := cm["delta"].(map[string]any); ok {
			if r.rewriteDelta(idx, delta) {
				changed = true
			}
		}
		if fr := cm["finish_reason"]; fr != nil {
			// The choice is done: whatever is still held is plain text. Put it
			// into this same chunk so it precedes the finish signal.
			if r.flushChoiceInto(idx, cm) {
				changed = true
			}
		}
	}
	return changed
}

func (r *Rewriter) rewriteDelta(idx int, d map[string]any) bool {
	changed := false
	set := func(m map[string]any, field string, k chanKey) {
		s, ok := m[field].(string)
		if !ok {
			return
		}
		if out := r.push(k, s); out != s {
			m[field] = out
			changed = true
		}
	}
	set(d, "content", chanKey{choice: idx, kind: chContent})
	set(d, "refusal", chanKey{choice: idx, kind: chRefusal})
	if a, ok := d["audio"].(map[string]any); ok {
		set(a, "transcript", chanKey{choice: idx, kind: chTranscript})
	}
	if fc, ok := d["function_call"].(map[string]any); ok {
		set(fc, "arguments", chanKey{choice: idx, kind: chFnArgs})
	}
	if tcs, ok := d["tool_calls"].([]any); ok {
		for i, t := range tcs {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			if fn, ok := tm["function"].(map[string]any); ok {
				set(fn, "arguments", chanKey{choice: idx, kind: chArgs, tool: intOf(tm["index"], i)})
			}
		}
	}
	return changed
}

// flushChoiceInto appends the held text of every channel of choice idx to the
// matching fields of cm's delta, creating them as needed.
func (r *Rewriter) flushChoiceInto(idx int, cm map[string]any) bool {
	keys := r.heldKeys(idx)
	if len(keys) == 0 {
		return false
	}
	delta, ok := cm["delta"].(map[string]any)
	if !ok {
		delta = map[string]any{}
		cm["delta"] = delta
	}
	for _, k := range keys {
		appendTail(delta, k, r.chans[k].takeHeld(r.ctx))
	}
	return true
}

func (ch *channel) takeHeld(ctx context.Context) string {
	t := ch.held
	ch.held = ""
	return ch.r.Text(ctx, t)
}

// heldKeys returns the channels of choice idx (or of every choice when idx < 0)
// that hold text, in a stable order.
func (r *Rewriter) heldKeys(idx int) []chanKey {
	var keys []chanKey
	for k, ch := range r.chans {
		if ch.held != "" && (idx < 0 || k.choice == idx) {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.choice != b.choice {
			return a.choice < b.choice
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.tool < b.tool
	})
	return keys
}

// flushAll emits synthetic chunks carrying any held text. It runs before
// [DONE], before an error frame, and at end of input.
func (r *Rewriter) flushAll() {
	keys := r.heldKeys(-1)
	for i := 0; i < len(keys); {
		j := i
		for j < len(keys) && keys[j].choice == keys[i].choice {
			j++
		}
		delta := map[string]any{}
		for _, k := range keys[i:j] {
			appendTail(delta, k, r.chans[k].takeHeld(r.ctx))
		}
		chunk := map[string]any{"choices": []any{map[string]any{
			"index": keys[i].choice, "delta": delta, "finish_reason": nil,
		}}}
		for k, v := range r.meta {
			chunk[k] = v
		}
		if _, ok := chunk["object"]; !ok {
			chunk["object"] = "chat.completion.chunk"
		}
		if enc, err := jsonx.Encode(chunk); err == nil {
			r.out.WriteString("data: ")
			r.out.Write(enc)
			r.out.WriteString("\n\n")
		}
		i = j
	}
}

// appendTail adds text to the delta field that channel k feeds.
func appendTail(delta map[string]any, k chanKey, tail string) {
	str := func(m map[string]any, field string) {
		prev, _ := m[field].(string)
		m[field] = prev + tail
	}
	switch k.kind {
	case chContent:
		str(delta, "content")
	case chRefusal:
		str(delta, "refusal")
	case chTranscript:
		a, ok := delta["audio"].(map[string]any)
		if !ok {
			a = map[string]any{}
			delta["audio"] = a
		}
		str(a, "transcript")
	case chFnArgs:
		fc, ok := delta["function_call"].(map[string]any)
		if !ok {
			fc = map[string]any{}
			delta["function_call"] = fc
		}
		str(fc, "arguments")
	case chArgs:
		tcs, _ := delta["tool_calls"].([]any)
		for i, t := range tcs {
			if tm, ok := t.(map[string]any); ok && intOf(tm["index"], i) == k.tool {
				fn, ok := tm["function"].(map[string]any)
				if !ok {
					fn = map[string]any{}
					tm["function"] = fn
				}
				str(fn, "arguments")
				return
			}
		}
		delta["tool_calls"] = append(tcs, map[string]any{
			"index":    json.Number(strconv.Itoa(k.tool)),
			"function": map[string]any{"arguments": tail},
		})
	}
}

func intOf(v any, def int) int {
	switch n := v.(type) {
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
	case int:
		return n
	}
	return def
}

// decodeBracketEscapes rewrites the JSON escapes for "<" and ">" (a backslash,
// the letter u, then 003c or 003e in either case) into the characters
// themselves. Replacing them is safe inside JSON text: both characters may
// appear unescaped in a JSON string, and the decoded value is unchanged. Every
// other escape is copied as a pair, so an escaped backslash followed by u003c
// is not misread. A trailing, incomplete escape is returned in partial, to be
// joined with the next piece.
func decodeBracketEscapes(s string) (out, partial string) {
	if strings.IndexByte(s, '\\') < 0 {
		return s, ""
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+1 >= len(s) {
			return b.String(), s[i:]
		}
		if s[i+1] == 'u' {
			if i+6 > len(s) {
				return b.String(), s[i:]
			}
			switch strings.ToLower(s[i+2 : i+6]) {
			case "003c":
				b.WriteByte('<')
				i += 6
				continue
			case "003e":
				b.WriteByte('>')
				i += 6
				continue
			}
		}
		b.WriteString(s[i : i+2])
		i += 2
	}
	return b.String(), ""
}
