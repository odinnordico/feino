package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// NewGitTools returns the git tool suite: git_status, git_log, git_diff,
// and git_blame. All tools are read-only.
func NewGitTools(logger *slog.Logger) []Tool {
	return []Tool{
		newGitStatusTool(logger),
		newGitLogTool(logger),
		newGitDiffTool(logger),
		newGitBlameTool(logger),
	}
}

const gitTimeout = 30 * time.Second

// runGit executes a git command in repoPath (or the process working directory
// when repoPath is empty) and returns trimmed combined output.
func runGit(repoPath string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	if repoPath != "" {
		cmd.Dir = repoPath
	}
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("git %s: timed out after %s", args[0], gitTimeout)
	}
	return strings.TrimRight(string(out), "\n"), err
}

func newGitStatusTool(logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"repo_path": map[string]any{
				"type":        "string",
				"description": "Path to the git repository root. Defaults to the current working directory.",
			},
		},
	}

	return NewTool(
		"git_status",
		"Return the short-format git status of a repository (equivalent to git status --short).",
		schema,
		func(params map[string]any) ToolResult {
			repoPath, _ := getString(params, "repo_path")
			logger.Debug("running git status", "path", repoPath)
			output, err := runGit(repoPath, "status", "--short")
			if err != nil {
				logger.Error("git status failed", "path", repoPath, "error", err)
				return NewToolResult(output, fmt.Errorf("git_status: %w", err))
			}
			return NewToolResult(output, nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

func newGitLogTool(logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"repo_path": map[string]any{
				"type":        "string",
				"description": "Path to the git repository root. Defaults to the current working directory.",
			},
			"n": map[string]any{
				"type":        "integer",
				"description": "Number of commits to return. Defaults to 20.",
			},
		},
	}

	return NewTool(
		"git_log",
		"Return the git commit log in one-line format (equivalent to git log --oneline -n N).",
		schema,
		func(params map[string]any) ToolResult {
			repoPath, _ := getString(params, "repo_path")
			n := getInt(params, "n", 20)
			logger.Debug("running git log", "path", repoPath, "n", n)
			output, err := runGit(repoPath, "log", "--oneline", "-n", strconv.Itoa(n))
			if err != nil {
				logger.Error("git log failed", "path", repoPath, "error", err)
				return NewToolResult(output, fmt.Errorf("git_log: %w", err))
			}
			return NewToolResult(output, nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

func newGitDiffTool(logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"repo_path": map[string]any{
				"type":        "string",
				"description": "Path to the git repository root. Defaults to the current working directory.",
			},
			"ref": map[string]any{
				"type":        "string",
				"description": "Optional git ref (commit hash, branch, or tag) to diff against. When omitted, diffs the working tree against the index.",
			},
		},
	}

	return NewTool(
		"git_diff",
		"Return the git diff output. When ref is provided, diffs the working tree against that ref.",
		schema,
		func(params map[string]any) ToolResult {
			repoPath, _ := getString(params, "repo_path")
			args := []string{"diff"}
			if ref, ok := getString(params, "ref"); ok && ref != "" {
				args = append(args, ref)
			}
			logger.Debug("running git diff", "path", repoPath, "args", args)
			output, err := runGit(repoPath, args...)
			if err != nil {
				logger.Error("git diff failed", "path", repoPath, "error", err)
				return NewToolResult(output, fmt.Errorf("git_diff: %w", err))
			}
			return NewToolResult(output, nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

func newGitBlameTool(logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Path to the file to blame, relative to repo_path.",
			},
			"repo_path": map[string]any{
				"type":        "string",
				"description": "Path to the git repository root. Defaults to the current working directory.",
			},
		},
		"required": []string{"file_path"},
	}

	return NewTool(
		"git_blame",
		"Return git blame output for a file, showing the commit and author for each line.",
		schema,
		func(params map[string]any) ToolResult {
			filePath, ok := getString(params, "file_path")
			if !ok || filePath == "" {
				return NewToolResult("", fmt.Errorf("git_blame: 'file_path' parameter is required"))
			}
			repoPath, _ := getString(params, "repo_path")
			logger.Debug("running git blame", "path", repoPath, "file", filePath)
			output, err := runGit(repoPath, "blame", filePath)
			if err != nil {
				logger.Error("git blame failed", "path", repoPath, "file", filePath, "error", err)
				return NewToolResult(output, fmt.Errorf("git_blame: %w", err))
			}
			return NewToolResult(output, nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}
