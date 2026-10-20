// Command pseudonymbench is a reverse proxy that masks PII in OpenAI-style
// chat-completions requests and restores it in the responses.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/vikasgowda3007/pseudonymbench/internal/mask"
	"github.com/vikasgowda3007/pseudonymbench/internal/proxy"
	"github.com/vikasgowda3007/pseudonymbench/internal/vault"
)

const keyEnv = "PSEUDONYMBENCH_KEY"

func main() {
	if err := run(); err != nil {
		log.Fatalf("pseudonymbench: %v", err)
	}
}

func run() error {
	var (
		listen    = flag.String("listen", env("PSEUDONYMBENCH_LISTEN", ":8080"), "address to listen on")
		upstream  = flag.String("upstream", env("PSEUDONYMBENCH_UPSTREAM", "https://api.openai.com"), "upstream API base URL")
		vaultKind = flag.String("vault", env("PSEUDONYMBENCH_VAULT", "memory"), "token vault: memory or redis")
		redisURL  = flag.String("redis-url", env("PSEUDONYMBENCH_REDIS_URL", "redis://localhost:6379/0"), "Redis URL when -vault=redis")
		redisLua  = flag.Bool("redis-lua", true, "allocate tokens with one Lua script (about 2.6x faster than the HSETNX sequence on a new value); -redis-lua=false for Redis deployments that forbid scripts")
		redisPipe = flag.Bool("redis-batch", true, "with Lua allocation, send all of a request's new values to Redis in one pipelined round trip; -redis-batch=false allocates them with concurrent calls")
		ttl       = flag.Duration("ttl", 15*time.Minute, "vault session lifetime, refreshed on each masked request")
		policy    = flag.String("policy", env("PSEUDONYMBENCH_POLICY", ""), "per-kind actions, e.g. CARD=block,PHONE=off (kinds: CARD IBAN EMAIL SSN PHONE)")
		allowURLs = flag.Bool("allow-urls", false, "restore tokens that appear inside URLs (off by default: exfiltration risk)")
		maxBody   = flag.Int64("max-body", 8<<20, "maximum request body in bytes")
		audit     = flag.String("audit", env("PSEUDONYMBENCH_AUDIT", "-"), "audit log file, '-' for stdout, 'off' to disable")
		counts    = flag.Bool("expose-counts", true, "send per-kind masked counts in the "+proxy.MaskedHeader+" response header")
	)
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: pseudonymbench [flags]\n\nThe master key is read from $%s (64 hex characters or base64 of 32 bytes).\nGenerate one with: openssl rand -hex 32\n\nFlags:\n", keyEnv)
		flag.PrintDefaults()
	}
	flag.Parse()

	up, err := url.Parse(*upstream)
	if err != nil || (up.Scheme != "http" && up.Scheme != "https") || up.Host == "" {
		return fmt.Errorf("-upstream must be an http(s) URL, got %q", *upstream)
	}
	pol, err := mask.ParsePolicy(*policy)
	if err != nil {
		return err
	}

	keys, err := loadKeys(*vaultKind)
	if err != nil {
		return err
	}
	var v vault.Vault
	switch *vaultKind {
	case "memory":
		v = vault.NewMemory(vault.MemoryOptions{TTL: *ttl, IndexKey: keys.Index})
	case "redis":
		opts, err := redis.ParseURL(*redisURL)
		if err != nil {
			return fmt.Errorf("-redis-url: %w", err)
		}
		sealer, err := vault.NewSealer(keys.Seal)
		if err != nil {
			return err
		}
		v, err = vault.NewRedis(vault.RedisOptions{Client: redis.NewClient(opts), Sealer: sealer, IndexKey: keys.Index, TTL: *ttl, Lua: *redisLua, NoBatch: !*redisPipe})
		if err != nil {
			return err
		}
		if err := pingRedis(v); err != nil {
			return fmt.Errorf("redis: %w", err)
		}
	default:
		return fmt.Errorf("-vault must be memory or redis, got %q", *vaultKind)
	}
	defer v.Close()

	auditLog, closeAudit, err := openAudit(*audit)
	if err != nil {
		return err
	}
	defer closeAudit()

	h, err := proxy.New(proxy.Config{
		Upstream: up, Masker: mask.New(pol), Vault: v, SessionKey: keys.Session,
		AllowURLs: *allowURLs, MaxBodyBytes: *maxBody, ExposeCounts: *counts, Audit: auditLog,
	})
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr: *listen, Handler: h,
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: streamed responses can run for minutes.
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()

	log.Printf("pseudonymbench: listening on %s, upstream %s, vault %s", *listen, up.Redacted(), *vaultKind)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// loadKeys reads the master key. Redis needs a stable key so every instance
// agrees on it; the in-memory vault falls back to a random one, which is fine
// because its sessions die with the process anyway.
func loadKeys(vaultKind string) (vault.Keys, error) {
	raw := os.Getenv(keyEnv)
	if raw == "" {
		if vaultKind == "redis" {
			return vault.Keys{}, fmt.Errorf("$%s is required with -vault=redis (generate one with: openssl rand -hex 32)", keyEnv)
		}
		log.Printf("pseudonymbench: $%s not set; using a random key for this process", keyEnv)
		return vault.DeriveKeys(vault.GenerateKey()), nil
	}
	master, err := vault.ParseKey(raw)
	if err != nil {
		return vault.Keys{}, fmt.Errorf("$%s: %w", keyEnv, err)
	}
	return vault.DeriveKeys(master), nil
}

func pingRedis(v vault.Vault) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := v.Restore(ctx, "ping", "<EMAIL_00000000>")
	return err
}

func openAudit(dest string) (*slog.Logger, func(), error) {
	var w io.Writer
	closeFn := func() {}
	switch dest {
	case "off":
		return nil, closeFn, nil
	case "-", "":
		w = os.Stdout
	default:
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, closeFn, fmt.Errorf("audit log: %w", err)
		}
		w, closeFn = f, func() { f.Close() }
	}
	return slog.New(slog.NewJSONHandler(w, nil)), closeFn, nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
