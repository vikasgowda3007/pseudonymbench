// Package proxy is the HTTP front end: it masks chat-completions requests,
// forwards them upstream, and restores tokens in the response, streamed or not.
//
// The proxy fails closed. A request it cannot scan (bad JSON, compressed body,
// unknown path, vault down) is rejected and never forwarded.
package proxy

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vikasgowda3007/pseudonymbench/internal/mask"
	"github.com/vikasgowda3007/pseudonymbench/internal/stream"
	"github.com/vikasgowda3007/pseudonymbench/internal/vault"
)

// SessionHeader lets a client group requests into one vault session, so the
// same value keeps the same token across turns. It is stripped before the
// request goes upstream.
const SessionHeader = "X-Pseudonym-Session"

// MaskedHeader reports how many values were masked, by kind. Counts only.
const MaskedHeader = "X-Pseudonymbench-Masked"

const chatPath = "/v1/chat/completions"

var sessionIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Config configures a Handler.
type Config struct {
	Upstream   *url.URL
	Masker     *mask.Masker
	Vault      vault.Vault
	SessionKey []byte // derives vault session ids from caller identity

	AllowURLs    bool  // restore tokens that appear inside URLs
	MaxBodyBytes int64 // request body limit; default 8 MiB
	// MaxRewriteBytes bounds a non-streamed response the proxy will buffer in
	// order to restore it; larger ones fail with 502. Default 64 MiB.
	MaxRewriteBytes int
	ExposeCounts    bool // send MaskedHeader on responses

	Audit     *slog.Logger // nil disables audit logging
	Transport http.RoundTripper
}

// Handler is the proxy. It implements http.Handler.
type Handler struct {
	cfg    Config
	client *http.Client
}

// New validates cfg and returns a Handler.
func New(cfg Config) (*Handler, error) {
	if cfg.Upstream == nil || cfg.Upstream.Host == "" {
		return nil, errors.New("proxy: upstream URL is required")
	}
	if cfg.Masker == nil || cfg.Vault == nil || len(cfg.SessionKey) == 0 {
		return nil, errors.New("proxy: masker, vault and session key are required")
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = 8 << 20
	}
	if cfg.MaxRewriteBytes <= 0 {
		cfg.MaxRewriteBytes = 64 << 20
	}
	rt := cfg.Transport
	if rt == nil {
		rt = &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConns:        512,
			MaxIdleConnsPerHost: 256,
			IdleConnTimeout:     90 * time.Second,
			DisableCompression:  true,
			ForceAttemptHTTP2:   true,
		}
	}
	return &Handler{cfg: cfg, client: &http.Client{
		Transport:     rt,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := &record{requestID: requestID(r), path: r.URL.Path, start: time.Now(), outcome: "ok"}
	w.Header().Set("X-Request-Id", rec.requestID)
	defer func() {
		rec.total = time.Since(rec.start)
		rec.log(h.cfg.Audit)
	}()

	switch {
	case r.URL.Path == "/healthz" && r.Method == http.MethodGet:
		rec.status = http.StatusOK
		w.WriteHeader(rec.status)
		io.WriteString(w, "ok\n")
	case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
		h.passthroughGET(w, r, rec)
	case r.URL.Path == chatPath && r.Method == http.MethodPost:
		h.chat(w, r, rec)
	case r.URL.Path == chatPath:
		h.fail(w, rec, http.StatusMethodNotAllowed, "method_not_allowed", "use POST", "method_not_allowed")
	default:
		h.fail(w, rec, http.StatusForbidden, "path_not_allowed",
			"pseudonymbench only proxies "+chatPath+" and GET /v1/models; other paths could carry unscanned PII", "path_not_allowed")
	}
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request, rec *record) {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		h.fail(w, rec, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", "unsupported_media_type")
		return
	}
	if ce := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding"))); ce != "" && ce != "identity" {
		h.fail(w, rec, http.StatusUnsupportedMediaType, "unsupported_encoding",
			"compressed request bodies cannot be scanned; send them uncompressed", "unsupported_encoding")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, h.cfg.MaxBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			h.fail(w, rec, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds the configured limit", "body_too_large")
		} else {
			h.fail(w, rec, http.StatusBadRequest, "bad_request", "could not read request body", "bad_request")
		}
		return
	}
	rec.bytesIn = int64(len(body))

	sid, err := h.sessionID(r)
	if err != nil {
		h.fail(w, rec, http.StatusBadRequest, "bad_session", err.Error(), "bad_session")
		return
	}
	rec.session = shortHash(sid)
	sess := mask.NewSession(h.cfg.Vault, sid)
	if r.Header.Get(SessionHeader) == "" {
		// A per-request session can never be looked up again: drop it once the
		// response (including a whole stream) is done, instead of at the TTL.
		defer h.cfg.Vault.Forget(context.Background(), sid)
	}

	t0 := time.Now()
	masked, rep, err := h.cfg.Masker.Request(r.Context(), sess, body)
	rec.maskDur = time.Since(t0)
	rec.masked = rep.Masked
	if err != nil {
		var blocked *mask.BlockedError
		switch {
		case errors.As(err, &blocked):
			rec.blocked = blocked.Kinds
			h.fail(w, rec, http.StatusUnprocessableEntity, "pii_blocked", blocked.Error(), "blocked")
		case errors.Is(err, mask.ErrInvalidBody):
			h.fail(w, rec, http.StatusBadRequest, "invalid_body", "request body must be a JSON object", "invalid_body")
		default:
			// Vault down or full: the request cannot be masked, so it is not sent.
			h.fail(w, rec, http.StatusServiceUnavailable, "vault_unavailable", "could not mask the request; it was not forwarded", "vault_error")
		}
		return
	}

	if rep.Total() == 0 {
		masked = body // nothing to replace: forward the client's bytes untouched
	}

	req, err := h.upstreamRequest(r, masked)
	if err != nil {
		h.fail(w, rec, http.StatusInternalServerError, "internal", "could not build upstream request", "internal")
		return
	}
	tUp := time.Now()
	resp, err := h.client.Do(req)
	rec.upstreamMS = time.Since(tUp)
	if err != nil {
		h.fail(w, rec, http.StatusBadGateway, "upstream_unreachable", "upstream request failed", "upstream_error")
		return
	}
	defer resp.Body.Close()
	if h.cfg.ExposeCounts {
		w.Header().Set(MaskedHeader, countsHeader(rep.Masked))
	}
	h.respond(w, r, resp, sess, rec)
}

// respond relays the upstream response, restoring tokens on the way.
func (h *Handler) respond(w http.ResponseWriter, r *http.Request, resp *http.Response, sess *mask.Session, rec *record) {
	copyHeaders(w.Header(), resp.Header)
	rec.status = resp.StatusCode
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	encoded := resp.Header.Get("Content-Encoding") != "" && !strings.EqualFold(resp.Header.Get("Content-Encoding"), "identity")

	switch {
	case encoded:
		// A compressed body can't be rewritten. Tokens stay visible; no PII is
		// involved, since the body came from upstream.
		rec.outcome = "restore_skipped_encoding"
		h.relay(w, resp, rec, resp.Body)

	case ct == "text/event-stream":
		rec.stream = true
		w.Header().Del("Content-Length")
		rw := stream.New(r.Context(), resp.Body, sess, stream.Options{AllowURLs: h.cfg.AllowURLs})
		h.relay(w, resp, rec, rw)
		st := rw.Stats()
		rec.restored, rec.unknown, rec.urlSkipped, rec.restoreErr = st.Restored, st.Unknown, st.URLSkipped, st.Errors

	case ct == "application/json":
		raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(h.cfg.MaxRewriteBytes)+1))
		if err != nil {
			h.fail(w, rec, http.StatusBadGateway, "upstream_read", "could not read upstream response", "upstream_error")
			return
		}
		if len(raw) > h.cfg.MaxRewriteBytes {
			// Forwarding a truncated body as a success would corrupt it silently.
			h.fail(w, rec, http.StatusBadGateway, "upstream_too_large", "upstream response is too large to rewrite", "upstream_error")
			return
		}
		rs := &mask.Restorer{Session: sess, AllowURLs: h.cfg.AllowURLs}
		out, err := rs.RestoreJSON(r.Context(), raw)
		if err != nil {
			out = raw // not parseable: forward as received
		}
		rec.restored, rec.unknown, rec.urlSkipped, rec.restoreErr = rs.Stats.Restored, rs.Stats.Unknown, rs.Stats.URLSkipped, rs.Stats.Errors
		w.Header().Set("Content-Length", strconv.Itoa(len(out)))
		h.relay(w, resp, rec, bytes.NewReader(out))

	default:
		h.relay(w, resp, rec, resp.Body)
	}
}

// relay writes the status line and copies body to the client, flushing after
// every write so streamed events are not held in a buffer.
func (h *Handler) relay(w http.ResponseWriter, resp *http.Response, rec *record, body io.Reader) {
	w.WriteHeader(resp.StatusCode)
	rc := http.NewResponseController(w)
	buf := make([]byte, 32<<10)
	first := true
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if first {
				rec.firstByte = time.Since(rec.start)
				first = false
			}
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			rec.bytesOut += int64(n)
			rc.Flush()
		}
		if err != nil {
			return
		}
	}
}

func (h *Handler) passthroughGET(w http.ResponseWriter, r *http.Request, rec *record) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.upstreamURL(r.URL.Path), nil)
	if err != nil {
		h.fail(w, rec, http.StatusInternalServerError, "internal", "could not build upstream request", "internal")
		return
	}
	copyHeaders(req.Header, r.Header)
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := h.client.Do(req)
	if err != nil {
		h.fail(w, rec, http.StatusBadGateway, "upstream_unreachable", "upstream request failed", "upstream_error")
		return
	}
	defer resp.Body.Close()
	copyHeaders(w.Header(), resp.Header)
	rec.status = resp.StatusCode
	h.relay(w, resp, rec, resp.Body)
}

func (h *Handler) upstreamRequest(r *http.Request, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.upstreamURL(r.URL.Path), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	copyHeaders(req.Header, r.Header)
	req.Header.Del(SessionHeader)
	req.Header.Del("Content-Length")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "identity") // the response must be readable to be restored
	req.ContentLength = int64(len(body))
	return req, nil
}

func (h *Handler) upstreamURL(path string) string {
	// The upstream URL's own query string (for example Azure's ?api-version=)
	// is kept. The client's query string is not forwarded: it is a request field
	// the masker never sees.
	u := *h.cfg.Upstream
	u.Path = strings.TrimRight(u.Path, "/") + path
	u.RawPath = ""
	return u.String()
}

// sessionID derives the vault session. With a caller-supplied session header
// the id is an HMAC of the caller's credential and that header, so knowing or
// guessing a session name is not enough to read its tokens. Without the header
// every request gets its own session.
func (h *Handler) sessionID(r *http.Request) (string, error) {
	caller := r.Header.Get(SessionHeader)
	if caller == "" {
		var b [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		return hex.EncodeToString(b[:]), nil
	}
	if !sessionIDRE.MatchString(caller) {
		return "", errors.New(SessionHeader + " must match [A-Za-z0-9._-]{1,128}")
	}
	cred := sha256.Sum256([]byte(credentials(r)))
	m := hmac.New(sha256.New, h.cfg.SessionKey)
	m.Write(cred[:])
	m.Write([]byte{0})
	m.Write([]byte(caller))
	return hex.EncodeToString(m.Sum(nil)), nil
}

// credentialHeaders are the request headers that identify a caller to an
// OpenAI-style API: the standard bearer header, Azure's api-key, and the
// x-api-key convention. All of them go into the session identity, so two
// callers who authenticate differently never share a session even if they
// pick the same session name. Callers who send none share one namespace.
var credentialHeaders = []string{"Authorization", "Api-Key", "X-Api-Key"}

func credentials(r *http.Request) string {
	var parts []string
	for _, h := range credentialHeaders {
		parts = append(parts, r.Header.Get(h))
	}
	return strings.Join(parts, "\x00")
}

// fail writes an OpenAI-shaped error. The message never includes request content.
func (h *Handler) fail(w http.ResponseWriter, rec *record, status int, code, msg, outcome string) {
	rec.status, rec.outcome = status, outcome
	// Headers already copied from an upstream response (Content-Length,
	// Content-Encoding ...) describe a body we are not sending.
	hdr := w.Header()
	keep := map[string]string{"X-Request-Id": hdr.Get("X-Request-Id"), MaskedHeader: hdr.Get(MaskedHeader)}
	for k := range hdr {
		delete(hdr, k)
	}
	for k, v := range keep {
		if v != "" {
			hdr.Set(k, v)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := `{"error":{"message":` + quote(msg) + `,"type":"pseudonymbench_error","code":` + quote(code) + `}}` + "\n"
	n, _ := io.WriteString(w, body)
	rec.bytesOut += int64(n)
}

var hopHeaders = []string{
	"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
	"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

// copyHeaders copies src to dst minus hop-by-hop headers and anything the
// Connection header names.
func copyHeaders(dst, src http.Header) {
	skip := map[string]bool{}
	for _, h := range hopHeaders {
		skip[h] = true
	}
	for _, v := range src.Values("Connection") {
		for _, f := range strings.Split(v, ",") {
			skip[http.CanonicalHeaderKey(strings.TrimSpace(f))] = true
		}
	}
	for k, vs := range src {
		if skip[k] {
			continue
		}
		dst.Del(k)
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

func requestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-Id"); id != "" && len(id) <= 64 && sessionIDRE.MatchString(id) {
		return id
	}
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
