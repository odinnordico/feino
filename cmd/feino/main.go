package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/tui"
	"github.com/odinnordico/feino/internal/ui/repl"
	"github.com/odinnordico/feino/internal/web"
)

func main() {
	noTUI := flag.Bool("no-tui", false, "use plain stdin/stdout REPL instead of the TUI")
	webMode := flag.Bool("web", false, "start the web UI (Connect RPC + embedded React SPA)")
	webHost := flag.String("web-host", "127.0.0.1", "host to bind the web server to")
	webPort := flag.Int("web-port", 7700, "port for the web server")
	logLevel := flag.String("log-level", "", "log level: debug, info, warn, error (overrides config)")
	flag.Parse()

	cfgPath, err := config.DefaultConfigPath()
	if err != nil {
		slog.Error("failed to get default config path", "error", err)
		os.Exit(1)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	cfg = config.Merge(cfg, config.FromEnv())

	if *logLevel != "" {
		cfg.UI.LogLevel = *logLevel
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *webMode {
		setupLogger(cfg.UI.LogLevel)
		opts := web.Options{
			Host:      *webHost,
			Port:      *webPort,
			AllowCORS: !isLoopback(*webHost),
		}
		if err := web.Start(ctx, cfg, opts); err != nil && !isCancelled(err) {
			slog.Error("web server error", "error", err)
			os.Exit(1)
		}
		return
	}

	if *noTUI {
		setupLogger(cfg.UI.LogLevel)
		sess, err := app.New(cfg, app.WithLogger(slog.Default()))
		if err != nil {
			slog.Error("failed to start", "error", err)
			os.Exit(1)
		}
		if err := repl.Run(ctx, sess, os.Stdin, os.Stdout); err != nil && !isCancelled(err) {
			slog.Error("failed to run repl", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := tui.Run(ctx, cfg); err != nil && !isCancelled(err) {
		slog.Error("failed to run tui", "error", err)
		os.Exit(1)
	}
}

func setupLogger(level string) {
	l := config.ParseLogLevel(level)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: l})))
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isCancelled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
