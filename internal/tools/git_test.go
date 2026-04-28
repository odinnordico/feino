package tools

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// initGitRepo creates a temporary git repository with one initial commit.
func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	mustGit := func(ctx context.Context, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mustGit(ctx, "init")
	mustGit(ctx, "config", "user.email", "test@feino.test")
	mustGit(ctx, "config", "user.name", "Feino Test")

	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	mustGit(ctx, "add", ".")
	mustGit(ctx, "commit", "-m", "init")

	return dir
}

func TestGitStatus(t *testing.T) {
	tool := newGitStatusTool(slog.Default())

	t.Run("clean repo returns empty output", func(t *testing.T) {
		dir := initGitRepo(t)
		result := tool.Run(map[string]any{"repo_path": dir})
		if result.GetError() != nil {
			t.Fatalf("unexpected error: %v", result.GetError())
		}
		got, _ := result.GetContent().(string)
		if got != "" {
			t.Errorf("expected empty status for clean repo, got %q", got)
		}
	})

	t.Run("modified file shows in status", func(t *testing.T) {
		dir := initGitRepo(t)
		if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n// changed\n"), 0644); err != nil {
			t.Fatal(err)
		}
		result := tool.Run(map[string]any{"repo_path": dir})
		if result.GetError() != nil {
			t.Fatalf("unexpected error: %v", result.GetError())
		}
		got, _ := result.GetContent().(string)
		if !strings.Contains(got, "hello.go") {
			t.Errorf("expected hello.go in status output, got %q", got)
		}
	})
}

func TestGitLog(t *testing.T) {
	tool := newGitLogTool(slog.Default())

	t.Run("one commit shows init message", func(t *testing.T) {
		dir := initGitRepo(t)
		result := tool.Run(map[string]any{"repo_path": dir})
		if result.GetError() != nil {
			t.Fatalf("unexpected error: %v", result.GetError())
		}
		got, _ := result.GetContent().(string)
		if !strings.Contains(got, "init") {
			t.Errorf("expected 'init' in log output, got %q", got)
		}
	})

	t.Run("n=1 returns single line", func(t *testing.T) {
		dir := initGitRepo(t)
		result := tool.Run(map[string]any{"repo_path": dir, "n": 1})
		if result.GetError() != nil {
			t.Fatalf("unexpected error: %v", result.GetError())
		}
		got, _ := result.GetContent().(string)
		lines := strings.Split(strings.TrimSpace(got), "\n")
		if len(lines) != 1 {
			t.Errorf("expected 1 log line, got %d: %q", len(lines), got)
		}
	})

	t.Run("n as float64 (JSON-decoded)", func(t *testing.T) {
		dir := initGitRepo(t)
		result := tool.Run(map[string]any{"repo_path": dir, "n": float64(1)})
		if result.GetError() != nil {
			t.Fatalf("unexpected error: %v", result.GetError())
		}
	})
}

func TestGitDiff(t *testing.T) {
	tool := newGitDiffTool(slog.Default())

	t.Run("clean working tree returns empty diff", func(t *testing.T) {
		dir := initGitRepo(t)
		result := tool.Run(map[string]any{"repo_path": dir})
		if result.GetError() != nil {
			t.Fatalf("unexpected error: %v", result.GetError())
		}
		got, _ := result.GetContent().(string)
		if got != "" {
			t.Errorf("expected empty diff for clean repo, got %q", got)
		}
	})

	t.Run("staged change shows in diff HEAD", func(t *testing.T) {
		dir := initGitRepo(t)
		if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n// changed\n"), 0644); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "git", "add", ".")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git add: %v\n%s", err, out)
		}
		result := tool.Run(map[string]any{"repo_path": dir, "ref": "HEAD"})
		if result.GetError() != nil {
			t.Fatalf("unexpected error: %v", result.GetError())
		}
		got, _ := result.GetContent().(string)
		if !strings.Contains(got, "hello.go") {
			t.Errorf("expected hello.go in diff output, got %q", got)
		}
	})
}

func TestGitBlame(t *testing.T) {
	tool := newGitBlameTool(slog.Default())

	t.Run("blame existing file contains commit info", func(t *testing.T) {
		dir := initGitRepo(t)
		result := tool.Run(map[string]any{"repo_path": dir, "file_path": "hello.go"})
		if result.GetError() != nil {
			t.Fatalf("unexpected error: %v", result.GetError())
		}
		got, _ := result.GetContent().(string)
		if !strings.Contains(got, "package main") {
			t.Errorf("expected source line in blame output, got %q", got)
		}
	})

	t.Run("blame non-existent file returns error", func(t *testing.T) {
		dir := initGitRepo(t)
		result := tool.Run(map[string]any{"repo_path": dir, "file_path": "ghost.go"})
		if result.GetError() == nil {
			t.Fatal("expected error for non-existent file, got nil")
		}
	})

	t.Run("missing file_path param returns error", func(t *testing.T) {
		result := tool.Run(map[string]any{})
		if result.GetError() == nil {
			t.Fatal("expected error for missing file_path, got nil")
		}
	})
}
