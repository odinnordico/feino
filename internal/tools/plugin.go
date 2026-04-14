// Script-plugin sub-system.
//
// A script plugin is a pair of files in the user's plugins directory:
//
//	~/.feino/plugins/
//	├── fetch_url          ← executable (any language, must have exec bit)
//	└── fetch_url.json     ← JSON manifest describing the tool
//
// # Protocol
//
// feino passes the tool parameters as a JSON object on stdin and reads a JSON
// response from stdout:
//
//	stdin  → {"url":"https://example.com"}
//	stdout → {"content":"<html>…</html>"}            ← success
//	stdout → {"content":"timeout","is_error":true}   ← failure
//
// If stdout is not valid JSON it is treated as plain-text content (success).
// Stderr is captured and appended to the error message on non-zero exit.
//
// Manifest fields
//
//	name             – tool name exposed to the model (default: filename stem)
//	description      – shown to the model in the system prompt
//	permission_level – "read" (default) | "write" | "bash" | "danger_zone"
//	parameters       – JSON Schema object describing the tool's inputs
//	timeout_seconds  – execution cap in seconds (default: 30)
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// pluginManifest is the JSON descriptor that lives alongside each plugin executable.
type pluginManifest struct {
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	PermissionLevel string         `json:"permission_level"` // "read"|"write"|"bash"|"danger_zone"
	Parameters      map[string]any `json:"parameters"`
	TimeoutSeconds  int            `json:"timeout_seconds"`
}

// pluginResponse is the JSON structure plugins must write to stdout.
type pluginResponse struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error"`
}

// ScriptPlugin implements tools.Tool by invoking an external executable.
// Parameters are passed as JSON on stdin; the plugin writes JSON (or plain
// text) to stdout.
type ScriptPlugin struct {
	name        string
	description string
	parameters  map[string]any
	level       int
	execPath    string
	timeout     time.Duration
	logger      *slog.Logger
}

func (p *ScriptPlugin) GetName() string               { return p.name }
func (p *ScriptPlugin) GetDescription() string        { return p.description }
func (p *ScriptPlugin) GetParameters() map[string]any { return p.parameters }
func (p *ScriptPlugin) PermissionLevel() int          { return p.level }
func (p *ScriptPlugin) GetLogger() *slog.Logger       { return p.logger }

// Run invokes the plugin executable with params marshalled as JSON on stdin.
// Stdout is expected to be a JSON pluginResponse; plain text is accepted too.
func (p *ScriptPlugin) Run(params map[string]any) ToolResult {
	input, err := json.Marshal(params)
	if err != nil {
		return NewToolResult("", fmt.Errorf("plugin %q: marshal params: %w", p.name, err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.execPath)
	cmd.Stdin = bytes.NewReader(input)
	// WaitDelay forces the stdout/stderr pipes closed after the context
	// deadline fires, even when orphaned grandchild processes (e.g. the
	// `sleep` spawned by a shell script) still hold the write end open.
	cmd.WaitDelay = p.timeout

	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return NewToolResult("", fmt.Errorf("plugin %q: timed out after %s", p.name, p.timeout))
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return NewToolResult("", fmt.Errorf("plugin %q: %w\nstderr: %s",
				p.name, err, strings.TrimSpace(string(exitErr.Stderr))))
		}
		return NewToolResult("", fmt.Errorf("plugin %q: %w", p.name, err))
	}

	var resp pluginResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		// Plugin wrote plain text — treat as successful content.
		return NewToolResult(strings.TrimSpace(string(out)), nil)
	}
	if resp.IsError {
		return NewToolResult(resp.Content, fmt.Errorf("plugin %q: %s", p.name, resp.Content))
	}
	return NewToolResult(resp.Content, nil)
}

// permLevelFromString maps a manifest permission_level string to a PermLevel*
// constant. Unknown values and the empty string default to PermLevelRead.
func permLevelFromString(s string) int {
	switch s {
	case "write":
		return PermLevelWrite
	case "bash":
		return PermLevelBash
	case "danger_zone":
		return PermLevelDangerZone
	default:
		return PermLevelRead
	}
}

// LoadPlugins discovers script plugins in dir and returns them as []Tool.
//
// Discovery rules:
//   - Walk dir (non-recursive) for *.json manifest files.
//   - For each manifest, look for a matching executable: first an exact-name
//     match (<stem>), then any file whose name stem matches (<stem>.<ext>).
//   - The file must have at least one executable bit set (0o111).
//   - Plugins that cannot be loaded (missing executable, bad manifest, no exec
//     bit) are skipped with a warning; only successfully loaded plugins are
//     returned.
//
// If dir does not exist, a nil slice is returned without error — this is the
// expected first-run case.
func LoadPlugins(dir string, logger *slog.Logger) ([]Tool, error) {
	if logger == nil {
		logger = slog.Default()
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("plugins: read dir %q: %w", dir, err)
	}

	var out []Tool
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), ".json")
		manifestPath := filepath.Join(dir, entry.Name())

		execPath := findPluginExecutable(dir, stem, entries)
		if execPath == "" {
			logger.Warn("plugin: no executable found for manifest", "manifest", entry.Name())
			continue
		}

		data, err := os.ReadFile(manifestPath)
		if err != nil {
			logger.Warn("plugin: read manifest failed", "manifest", manifestPath, "error", err)
			continue
		}

		var m pluginManifest
		if err := json.Unmarshal(data, &m); err != nil {
			logger.Warn("plugin: parse manifest failed", "manifest", manifestPath, "error", err)
			continue
		}

		name := m.Name
		if name == "" {
			name = stem
		}
		timeout := time.Duration(m.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 30 * time.Second
		}

		p := &ScriptPlugin{
			name:        name,
			description: m.Description,
			parameters:  m.Parameters,
			level:       permLevelFromString(m.PermissionLevel),
			execPath:    execPath,
			timeout:     timeout,
			logger:      logger.With("plugin", name),
		}
		out = append(out, p)
		logger.Info("plugin loaded", "name", name, "exec", execPath, "permission_level", m.PermissionLevel)
	}
	return out, nil
}

// findPluginExecutable looks for a file in dir whose base name (without
// extension) equals stem and which has at least one executable bit set.
// The .json manifest itself is excluded. Returns the full path of the first
// match, or "" if none is found.
func findPluginExecutable(dir, stem string, entries []fs.DirEntry) string {
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if base != stem {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Mode()&0o111 != 0 {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}
