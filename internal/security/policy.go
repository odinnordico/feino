package security

import (
	"fmt"
	"regexp"

	"github.com/odinnordico/feino/internal/tools"
)

// PermissionLevel represents the minimum permission required to execute a tool.
// Levels are ordered: Read < Write < Bash < DangerZone.
type PermissionLevel int

const (
	PermissionRead       PermissionLevel = iota // non-mutating reads
	PermissionWrite                             // file mutations
	PermissionBash                              // shell execution
	PermissionDangerZone                        // destructive operations
)

var permissionLevelNames = []string{"read", "write", "bash", "danger_zone"}

func (l PermissionLevel) String() string {
	if l >= 0 && int(l) < len(permissionLevelNames) {
		return permissionLevelNames[l]
	}
	return fmt.Sprintf("permission(%d)", int(l))
}

// dangerousPatterns is compiled once at package initialization.
// Any shell command matching one of these patterns is escalated to DangerZone.
var dangerousPatterns = func() []*regexp.Regexp {
	raw := []string{
		`rm\s+-(?:[a-zA-Z]*[rR][a-zA-Z]*f|[a-zA-Z]*f[a-zA-Z]*[rR])[a-zA-Z]*`, // -rf, -fr, -Rf, -fR, …
		`git\s+push.*--force`,
		`(?i)DROP\s`,
		`(?i)TRUNCATE\s`,
		`shred\s`,
		`mkfs`,
		`dd\s+if=`,
		`:\(\)\s*\{:\|:&\};:`, // fork bomb
		`chmod\s+-R\s+777`,
	}
	compiled := make([]*regexp.Regexp, len(raw))
	for i, p := range raw {
		compiled[i] = regexp.MustCompile(p)
	}
	return compiled
}()

// isDangerous returns true if command matches any dangerous pattern.
func isDangerous(command string) bool {
	for _, re := range dangerousPatterns {
		if re.MatchString(command) {
			return true
		}
	}
	return false
}

// LevelForTool returns the effective permission level required to invoke t with params.
//
// Lookup order:
//  1. extra map — caller-supplied overrides keyed by tool name (highest priority)
//  2. tools.Classified — self-declared level on the tool itself
//  3. PermissionDangerZone — safe default for unclassified tools (e.g. MCP tools)
//
// DangerZone escalation is applied for shell_exec regardless of its declared base level:
// if the "command" parameter matches a dangerous pattern the effective level is DangerZone.
// Passing nil for params is safe. Passing nil for extra is also safe — nil map reads
// return the zero value without panicking.
func LevelForTool(t tools.Tool, params map[string]any, extra map[string]PermissionLevel) PermissionLevel {
	// 1. Caller override by name
	if level, ok := extra[t.GetName()]; ok {
		return escalate(t.GetName(), level, params)
	}

	// 2. Self-declared via the optional Classified interface
	if c, ok := t.(tools.Classified); ok {
		if l := c.PermissionLevel(); l >= 0 {
			return escalate(t.GetName(), PermissionLevel(l), params)
		}
	}

	// 3. Safe fallback
	return PermissionDangerZone
}

// escalate applies DangerZone promotion for shell_exec commands that match
// dangerous patterns. For all other tools the base level is returned unchanged.
func escalate(name string, base PermissionLevel, params map[string]any) PermissionLevel {
	if name == "shell_exec" {
		if cmd, ok := params["command"].(string); ok && isDangerous(cmd) {
			return PermissionDangerZone
		}
	}
	return base
}
