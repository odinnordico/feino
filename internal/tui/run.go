// Package tui provides the Bubble Tea terminal user interface for feino.
// Call Run to start the TUI; it handles first-run detection, the setup wizard,
// and the main chat loop.
package tui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/credentials"
	"github.com/odinnordico/feino/internal/i18n"
	"github.com/odinnordico/feino/internal/memory"
	ollamaprov "github.com/odinnordico/feino/internal/provider/ollama"
	"github.com/odinnordico/feino/internal/security"
	"github.com/odinnordico/feino/internal/tools"
	emailtools "github.com/odinnordico/feino/internal/tools/services/email"
	"github.com/odinnordico/feino/internal/tui/chat"
	"github.com/odinnordico/feino/internal/tui/theme"
	"github.com/odinnordico/feino/internal/tui/wizard"
)

// Run is the single entry point for the TUI. It:
//  1. Redirects slog to a file so warnings don't corrupt the TUI terminal.
//  2. Runs the setup wizard if no provider is configured.
//  3. Saves updated config to disk.
//  4. Creates an app.Session.
//  5. Starts the Bubble Tea program.
func Run(ctx context.Context, cfg config.Config) error {

	// Initialise i18n before anything renders. Language from config wins;
	// empty string auto-detects from $LANG / $LC_ALL.
	i18n.Init(cfg.UI.Language)

	// Redirect all slog output to a file to keep the TUI terminal clean.
	// The tiktoken "model not found" warning and other internal warnings go here.
	if logFile, err := openLogFile(); err == nil {
		defer func() { _ = logFile.Close() }()
		slog.SetDefault(slog.New(slog.NewTextHandler(logFile, &slog.HandlerOptions{
			Level: config.ParseLogLevel(cfg.UI.LogLevel),
		})))
	}

	if !config.HasCredentials(cfg) {
		res, err := wizard.Run(ctx, cfg)
		if err != nil {
			if errors.Is(err, wizard.ErrAborted) {
				fmt.Fprintln(os.Stderr, i18n.T("setup_cancelled"))
				return nil
			}
			return fmt.Errorf("tui: setup wizard: %w", err)
		}

		cfg = config.Merge(cfg, res.ToConfig())

		cfgPath, err := config.DefaultConfigPath()
		if err != nil {
			return fmt.Errorf("tui: default config path: %w", err)
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return fmt.Errorf("tui: save config: %w", err)
		}
		// Merge env again so runtime env vars override wizard-set values.
		cfg = config.Merge(cfg, config.FromEnv())
	}

	// Create the credential store. It lives in ~/.feino/ alongside the config.
	store := credentialStore()

	// Build session options: Ollama provider (if configured) + email tools (if enabled).
	opts := buildOllamaOpts(ctx, cfg, slog.Default())
	if cfg.Services.Email.Enabled != nil && *cfg.Services.Email.Enabled {
		emailToolList := emailtools.NewEmailTools(cfg.Services.Email, store, slog.Default())
		opts = append(opts, app.WithExtraTools(emailToolList...))
	}

	// Memory store: persists agent-learned facts across sessions.
	memStore := openMemoryStore(slog.Default())
	if memStore != nil {
		opts = append(opts, app.WithMemoryStore(memStore))
		opts = append(opts, app.WithExtraTools(tools.NewMemoryTools(memStore, slog.Default())...))
	}

	sess, err := app.New(cfg, opts...)
	if err != nil {
		return fmt.Errorf("tui: start session: %w\nHint: set ANTHROPIC_API_KEY, OPENAI_API_KEY, or GEMINI_API_KEY", err)
	}

	th := theme.FromConfig(cfg.UI.Theme)

	// Global zone manager for mouse zone tracking.
	zm := zone.New()

	chatModel := chat.New(sess, cfg, th, zm)
	chatModel.SetStore(store)
	if memStore != nil {
		chatModel.SetMemoryStore(memStore)
	}

	prog := tea.NewProgram(
		chatModel,
		tea.WithAltScreen(),
		tea.WithMouseAllMotion(),
	)

	// Wire session events → program. Must happen before prog.Run() is called.
	sess.Subscribe(func(e app.Event) {
		prog.Send(chat.SessionEventMsg{Event: e})
	})

	// Wire permission prompts → TUI. The ReAct goroutine blocks on the response
	// channel; the TUI resolves it when the user types y or n.
	sess.SetPermissionCallback(func(ctx context.Context, toolName string, required, allowed security.PermissionLevel) bool {
		ch := make(chan bool, 1)
		prog.Send(chat.PermissionRequestMsg{
			ToolName: toolName,
			Required: required.String(),
			Allowed:  allowed.String(),
			Response: ch,
		})
		select {
		case approved := <-ch:
			return approved
		case <-ctx.Done():
			return false
		}
	})

	chatModel.SetProgram(prog)

	_, err = prog.Run()
	return err
}

// feinoDir returns the path to the ~/.feino directory, creating it if needed.
func feinoDir() (string, error) {
	return config.FeinoDir()
}

// openLogFile opens (or creates) ~/.feino/feino.log for appending.
// Logs are written here so the TUI terminal stays clean.
func openLogFile() (*os.File, error) {
	dir, err := feinoDir()
	if err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "feino.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

// buildOllamaOpts returns a WithProviders option for Ollama when the config
// specifies an Ollama default model. buildProviders inside app.New deliberately
// skips Ollama (starting the daemon is a side effect), so callers must inject
// it explicitly.
//
// As a side effect this sets OLLAMA_HOST in the process environment when a
// custom host is configured: the Ollama Go client reads that variable before
// constructing its HTTP client and provides no other way to override the host.
func buildOllamaOpts(ctx context.Context, cfg config.Config, logger *slog.Logger) []app.SessionOption {
	if cfg.Providers.Ollama.DefaultModel == "" {
		return nil
	}

	if cfg.Providers.Ollama.Host != "" {
		if err := os.Setenv("OLLAMA_HOST", cfg.Providers.Ollama.Host); err != nil {
			logger.Warn("could not set OLLAMA_HOST", "error", err)
		}
	}

	prov, err := ollamaprov.NewProvider(ctx, logger)
	if err != nil {
		logger.Warn("ollama provider unavailable", "error", err)
		return nil
	}
	return []app.SessionOption{app.WithProviders(prov)}
}

// credentialStore opens (or creates) the credential store in ~/.feino/.
// On any error it falls back to the default path resolution via credentials.New.
func credentialStore() credentials.Store {
	dir, err := feinoDir()
	if err != nil {
		return credentials.New("")
	}
	return credentials.New(dir)
}

// openMemoryStore opens (or creates) the memory store at ~/.feino/memory.json.
// Returns nil on any error so the rest of the TUI can proceed without memory.
func openMemoryStore(logger *slog.Logger) *memory.FileStore {
	p, err := memory.DefaultPath()
	if err != nil {
		logger.Warn("memory store: cannot determine path", "error", err)
		return nil
	}
	ms, err := memory.NewFileStore(p)
	if err != nil {
		logger.Warn("memory store: open failed", "path", p, "error", err)
		return nil
	}
	return ms
}
