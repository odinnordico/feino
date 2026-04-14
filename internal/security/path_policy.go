package security

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// pathSep is the OS path separator as a string, computed once at init time.
var pathSep = string(filepath.Separator)

// pathParamForTool maps a tool name to the params key that holds the filesystem
// path to check. Tools absent from this map are not subject to path enforcement.
var pathParamForTool = map[string]string{
	"file_read":  "path",
	"file_write": "path",
	"file_edit":  "path",
	"file_glob":  "pattern", // requires wildcard stripping; see extractCheckPath
	"git_blame":  "file_path",
	"git_status": "repo_path",
	"git_log":    "repo_path",
	"git_diff":   "repo_path",
}

// extractCheckPath derives the filesystem path to enforce from a tool invocation.
// Returns ("", false) when the tool has no path parameter or the parameter is absent,
// meaning the path check should be skipped entirely.
func extractCheckPath(toolName string, params map[string]any) (string, bool) {
	paramKey, ok := pathParamForTool[toolName]
	if !ok {
		return "", false
	}

	raw, _ := params[paramKey].(string)
	if raw == "" {
		return "", false
	}

	if toolName == "file_glob" {
		// Join with base_dir if provided — mirrors the tool's own runtime logic.
		if baseDir, _ := params["base_dir"].(string); baseDir != "" {
			raw = filepath.Join(baseDir, raw)
		}
		// Strip from the first wildcard character to recover the static directory.
		if idx := strings.IndexAny(raw, "*?"); idx >= 0 {
			raw = filepath.Dir(raw[:idx])
		}
		if raw == "" {
			raw = "."
		}
	}

	if toolName == "git_blame" {
		// file_path may be relative to repo_path; resolve them together.
		if repoPath, _ := params["repo_path"].(string); repoPath != "" {
			raw = filepath.Join(repoPath, raw)
		}
	}

	return raw, true
}

// PathPolicy holds a set of approved filesystem roots. Any path that equals an
// approved root or is a descendant of one is considered allowed.
//
// An empty PathPolicy (no roots added) denies every path check. Callers must
// call Allow at least once before the policy permits any path-sensitive tool.
//
// PathPolicy is safe for concurrent use.
type PathPolicy struct {
	mu    sync.RWMutex
	roots []string
}

// NewPathPolicy returns an empty PathPolicy. Wire it into a SecurityGate with
// WithPathPolicy, then populate it with Allow before any tool invocations.
func NewPathPolicy() *PathPolicy {
	return &PathPolicy{}
}

// Allow adds path as an approved root. Descendant paths are automatically
// approved. The path is resolved to an absolute, clean form; relative paths are
// resolved against the process working directory. Duplicate roots are silently
// ignored.
//
// Symlinks are not followed — the check is purely lexical.
func (p *PathPolicy) Allow(path string) error {
	abs, err := filepath.Abs(path) // Abs already calls Clean internally
	if err != nil {
		return fmt.Errorf("path policy: resolving %q: %w", path, err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if slices.Contains(p.roots, abs) {
		return nil // already present; skip duplicate
	}
	p.roots = append(p.roots, abs)
	return nil
}

// IsAllowed reports whether path is within any approved root.
// Returns false for an empty policy (no roots added).
func (p *PathPolicy) IsAllowed(path string) bool {
	candidate, err := filepath.Abs(path) // Abs already calls Clean internally
	if err != nil {
		return false
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, root := range p.roots {
		if candidate == root || strings.HasPrefix(candidate, root+pathSep) {
			return true
		}
	}
	return false
}
