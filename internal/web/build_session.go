package web

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/credentials"
	"github.com/odinnordico/feino/internal/memory"
	ollamaprov "github.com/odinnordico/feino/internal/provider/ollama"
	"github.com/odinnordico/feino/internal/tools"
	emailtools "github.com/odinnordico/feino/internal/tools/services/email"
)

// sessionAssets bundles everything constructed by BuildSession so callers
// receive a single struct instead of four return values.
type sessionAssets struct {
	sess    *app.Session
	sm      *SessionManager
	mhub    *metricsHub
	fileSvc *fileService
	store   credentials.Store
	mem     *memory.FileStore
	cfgPath string
}

// BuildSession constructs an app.Session with the same options as tui.Run:
// Ollama provider injection, email tools (when enabled), and the memory
// store. It is shared between the TUI and web runtimes to avoid duplication.
func BuildSession(cfg config.Config) (sessionAssets, error) {
	cfgPath, _ := config.DefaultConfigPath()
	store := credentialStore()

	opts := []app.SessionOption{app.WithLogger(slog.Default())}

	// Ollama must be injected explicitly because app.New skips it by default
	// (starting the daemon is a side effect of construction).
	if cfg.Providers.Ollama.DefaultModel != "" {
		if cfg.Providers.Ollama.Host != "" {
			if err := os.Setenv("OLLAMA_HOST", cfg.Providers.Ollama.Host); err != nil {
				slog.Warn("could not set OLLAMA_HOST", "error", err)
			}
		}
		if prov, err := ollamaprov.NewProvider(context.Background(), slog.Default()); err == nil {
			opts = append(opts, app.WithProviders(prov))
		} else {
			slog.Warn("ollama provider unavailable", "error", err)
		}
	}

	if cfg.Services.Email.Enabled != nil && *cfg.Services.Email.Enabled {
		emailToolList := emailtools.NewEmailTools(cfg.Services.Email, store, slog.Default())
		opts = append(opts, app.WithExtraTools(emailToolList...))
	}

	var memStore *memory.FileStore
	if ms, err := openMemoryStore(); err == nil && ms != nil {
		memStore = ms
		opts = append(opts, app.WithMemoryStore(memStore))
		opts = append(opts, app.WithExtraTools(tools.NewMemoryTools(memStore, slog.Default())...))
	}

	sess, err := app.New(cfg, opts...)
	if err != nil {
		return sessionAssets{}, err
	}

	sm := NewSessionManager(sess)
	mhub := newMetricsHub(sess)

	fileSvc, err := newFileService(cfg.Context.WorkingDir)
	if err != nil {
		return sessionAssets{}, fmt.Errorf("build_session: %w", err)
	}

	return sessionAssets{
		sess:    sess,
		sm:      sm,
		mhub:    mhub,
		fileSvc: fileSvc,
		store:   store,
		mem:     memStore,
		cfgPath: cfgPath,
	}, nil
}

// credentialStore opens the credential store in ~/.feino/.
func credentialStore() credentials.Store {
	dir, err := config.FeinoDir()
	if err != nil {
		return credentials.New("")
	}
	return credentials.New(dir)
}

// openMemoryStore opens (or creates) the memory store at ~/.feino/memory.json.
func openMemoryStore() (*memory.FileStore, error) {
	p, err := memory.DefaultPath()
	if err != nil {
		return nil, err
	}
	return memory.NewFileStore(p)
}
