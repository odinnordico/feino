package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/odinnordico/feino/gen/feino/v1/feinov1connect"
	"github.com/odinnordico/feino/internal/config"
)

// Options controls how the web server binds and which optional features are
// enabled.
type Options struct {
	Host      string // default "127.0.0.1"
	Port      int    // default 7700
	AllowCORS bool   // set true when Host is not loopback
}

// Start builds a session, registers the Connect RPC handler, mounts the
// embedded React SPA, then serves until ctx is cancelled.
func Start(ctx context.Context, cfg *config.Config, opts Options) error {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	if opts.Port == 0 {
		opts.Port = 7700
	}

	assets, err := BuildSession(cfg)
	if err != nil {
		return fmt.Errorf("web: build session: %w", err)
	}
	defer assets.fileSvc.Close()

	h := &FeinoServiceHandler{
		sess:    assets.sess,
		sm:      assets.sm,
		mhub:    assets.mhub,
		fileSvc: assets.fileSvc,
		store:   assets.store,
		mem:     assets.mem,
		cfg:     cfg,
		cfgPath: assets.cfgPath,
	}

	mux := http.NewServeMux()

	// ── Connect RPC ───────────────────────────────────────────────────────────
	var connectHandler http.Handler
	var connectOpts []connectOption
	if opts.AllowCORS {
		connectOpts = append(connectOpts, withCORSHeaders)
	}
	path, rpcHandler := feinov1connect.NewFeinoServiceHandler(h)
	connectHandler = applyMiddleware(rpcHandler, connectOpts...)
	mux.Handle(path, connectHandler)

	// ── Static SPA ────────────────────────────────────────────────────────────
	mux.Handle("/", spaHandler(EmbeddedFS()))

	// ── h2c (HTTP/2 cleartext) ────────────────────────────────────────────────
	srv := &http.Server{
		Addr:              net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port)),
		Handler:           h2c.NewHandler(mux, &http2.Server{}),
		ReadHeaderTimeout: 30 * time.Second,
	}

	// Shutdown when the context is cancelled.
	go func() { //nolint:gosec // G118: context.Background used intentionally for shutdown timeout; cancel is deferred immediately below
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			slog.Warn("web server shutdown error", "error", err)
		}
	}()

	slog.Info("web server listening", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("web: listen and serve: %w", err)
	}
	return nil
}

// ── middleware helpers ────────────────────────────────────────────────────────

type connectOption func(http.Handler) http.Handler

func applyMiddleware(h http.Handler, opts ...connectOption) http.Handler {
	for _, o := range opts {
		h = o(h)
	}
	return h
}

// withCORSHeaders adds permissive CORS headers required for browser clients
// connecting from origins other than the server's own origin. This is only
// enabled when --web-host is not a loopback address.
func withCORSHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers",
			"Content-Type, Connect-Protocol-Version, Connect-Timeout-Ms, Grpc-Timeout, X-Grpc-Web")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
