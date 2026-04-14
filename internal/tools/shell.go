package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// NewShellTools returns the shell tool suite (currently just shell_exec).
func NewShellTools(logger *slog.Logger) []Tool {
	return []Tool{newShellExecTool(logger)}
}

func newShellExecTool(logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute.",
			},
			"working_dir": map[string]any{
				"type":        "string",
				"description": "Directory to run the command in. Defaults to the process working directory.",
			},
			"timeout_seconds": map[string]any{
				"type":        "integer",
				"description": "Maximum execution time in seconds. Defaults to 30.",
			},
		},
		"required": []string{"command"},
	}

	return NewTool(
		"shell_exec",
		"Execute a shell command and return its combined stdout and stderr output.",
		schema,
		func(params map[string]any) ToolResult {
			cmd, ok := getString(params, "command")
			if !ok || strings.TrimSpace(cmd) == "" {
				return NewToolResult("", fmt.Errorf("shell_exec: 'command' parameter is required"))
			}

			timeoutSec := getInt(params, "timeout_seconds", 30)
			workingDir := getStringDefault(params, "working_dir", "")

			logger.Debug("executing shell command", "command", cmd, "working_dir", workingDir)

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
			defer cancel()

			c := exec.CommandContext(ctx, "sh", "-c", cmd)
			if workingDir != "" {
				c.Dir = workingDir
			}
			out, err := c.CombinedOutput()
			output := string(out)
			if err != nil {
				logger.Error("shell command failed", "command", cmd, "error", err)
				return NewToolResult(output, fmt.Errorf("shell_exec: command failed: %w", err))
			}
			return NewToolResult(output, nil)
		},
		WithPermissionLevel(PermLevelBash),
		WithLogger(logger),
	)
}
