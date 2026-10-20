package proxy

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vikasgowda3007/pseudonymbench/internal/detect"
)

// record is everything the audit log says about one request. It holds counts,
// kinds, timings and identifiers, never PII values, prompt text or headers.
type record struct {
	requestID string
	path      string
	status    int
	stream    bool

	session string // short hash of the vault session id, enough to correlate

	masked  map[detect.Kind]int
	blocked []detect.Kind

	restored   map[detect.Kind]int
	unknown    int
	urlSkipped int
	restoreErr int

	outcome string // "ok", "blocked", "invalid_body", "vault_error", "upstream_error", ...

	start      time.Time
	maskDur    time.Duration
	upstreamMS time.Duration // time to upstream response headers
	firstByte  time.Duration // time to first byte written to the client
	total      time.Duration

	bytesIn, bytesOut int64
}

func (r *record) log(l *slog.Logger) {
	if l == nil {
		return
	}
	attrs := []slog.Attr{
		slog.String("request_id", r.requestID),
		slog.String("path", r.path),
		slog.Int("status", r.status),
		slog.String("outcome", r.outcome),
		slog.Bool("stream", r.stream),
		slog.String("session", r.session),
		slog.Any("masked", countsAttr(r.masked)),
		slog.Float64("mask_ms", ms(r.maskDur)),
		slog.Float64("upstream_headers_ms", ms(r.upstreamMS)),
		slog.Float64("first_byte_ms", ms(r.firstByte)),
		slog.Float64("total_ms", ms(r.total)),
		slog.Int64("bytes_in", r.bytesIn),
		slog.Int64("bytes_out", r.bytesOut),
	}
	if len(r.blocked) > 0 {
		names := make([]string, len(r.blocked))
		for i, k := range r.blocked {
			names[i] = string(k)
		}
		attrs = append(attrs, slog.String("blocked", strings.Join(names, ",")))
	}
	if len(r.restored) > 0 || r.unknown > 0 || r.urlSkipped > 0 || r.restoreErr > 0 {
		attrs = append(attrs,
			slog.Any("restored", countsAttr(r.restored)),
			slog.Int("unknown_tokens", r.unknown),
			slog.Int("url_skipped", r.urlSkipped),
			slog.Int("restore_errors", r.restoreErr))
	}
	l.LogAttrs(context.Background(), slog.LevelInfo, "request", attrs...)
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// countsAttr renders kind counts as a map with stable string keys.
func countsAttr(m map[detect.Kind]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, n := range m {
		out[string(k)] = n
	}
	return out
}

// countsHeader renders counts as "CARD=1,EMAIL=2" for the response header.
func countsHeader(m map[detect.Kind]int) string {
	var parts []string
	for k, n := range m {
		if n > 0 {
			parts = append(parts, string(k)+"="+strconv.Itoa(n))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
