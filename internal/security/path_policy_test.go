package security

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ── PathPolicy.IsAllowed ──────────────────────────────────────────────────────

func TestPathPolicy_IsAllowed_ExactMatch(t *testing.T) {
	pp := NewPathPolicy()
	root := t.TempDir()
	_ = pp.Allow(root)

	if !pp.IsAllowed(root) {
		t.Errorf("exact root %q should be allowed", root)
	}
}

func TestPathPolicy_IsAllowed_ChildPath(t *testing.T) {
	pp := NewPathPolicy()
	root := t.TempDir()
	_ = pp.Allow(root)

	child := filepath.Join(root, "src", "main.go")
	if !pp.IsAllowed(child) {
		t.Errorf("child path %q should be allowed under root %q", child, root)
	}
}

func TestPathPolicy_IsAllowed_SiblingNotMatched(t *testing.T) {
	pp := NewPathPolicy()
	root := t.TempDir()
	_ = pp.Allow(root)

	// Construct a sibling by appending to the base name — avoids os.MkdirTemp.
	sibling := root + "_sibling"
	if pp.IsAllowed(sibling) {
		t.Errorf("sibling path %q should NOT be allowed under root %q", sibling, root)
	}
}

func TestPathPolicy_IsAllowed_ParentNotMatched(t *testing.T) {
	pp := NewPathPolicy()
	root := t.TempDir()
	_ = pp.Allow(root)

	parent := filepath.Dir(root)
	if pp.IsAllowed(parent) {
		t.Errorf("parent %q should NOT be allowed when only child %q is approved", parent, root)
	}
}

func TestPathPolicy_IsAllowed_UnrelatedPath(t *testing.T) {
	pp := NewPathPolicy()
	root := t.TempDir()
	_ = pp.Allow(root)

	if pp.IsAllowed("/etc/passwd") {
		t.Error("/etc/passwd should not be allowed")
	}
}

func TestPathPolicy_EmptyPolicyDeniesAll(t *testing.T) {
	pp := NewPathPolicy()
	if pp.IsAllowed("/any/path") {
		t.Error("empty PathPolicy should deny every path")
	}
}

func TestPathPolicy_MultipleRoots(t *testing.T) {
	pp := NewPathPolicy()
	root1 := t.TempDir()
	root2 := t.TempDir()
	_ = pp.Allow(root1)
	_ = pp.Allow(root2)

	if !pp.IsAllowed(filepath.Join(root1, "a.go")) {
		t.Errorf("child of root1 should be allowed")
	}
	if !pp.IsAllowed(filepath.Join(root2, "b.go")) {
		t.Errorf("child of root2 should be allowed")
	}
	if pp.IsAllowed("/home/other/file.go") {
		t.Error("unrelated path should not be allowed")
	}
}

func TestPathPolicy_Allow_NormalizesRelativePath(t *testing.T) {
	pp := NewPathPolicy()
	root := t.TempDir()
	// Allow uses filepath.Abs; pass an already-absolute path so the test is
	// portable, then verify that a child is correctly resolved.
	_ = pp.Allow(root)

	if !pp.IsAllowed(filepath.Join(root, "subdir", "file.txt")) {
		t.Errorf("normalized child should be allowed")
	}
}

func TestPathPolicy_TraversalInParam(t *testing.T) {
	pp := NewPathPolicy()
	root := t.TempDir()
	_ = pp.Allow(root)

	// A traversal that escapes the root after cleaning.
	traversal := filepath.Join(root, "..", "..", "etc", "passwd")
	if pp.IsAllowed(traversal) {
		t.Errorf("path traversal %q should not be allowed", traversal)
	}
}

// ── extractCheckPath ──────────────────────────────────────────────────────────

func TestExtractCheckPath_FileRead(t *testing.T) {
	path, ok := extractCheckPath("file_read", map[string]any{"path": "/home/user/project/main.go"})
	if !ok {
		t.Fatal("expected needsCheck=true for file_read")
	}
	if path != "/home/user/project/main.go" {
		t.Errorf("unexpected path: %q", path)
	}
}

func TestExtractCheckPath_ShellExecUnaffected(t *testing.T) {
	_, ok := extractCheckPath("shell_exec", map[string]any{"command": "ls"})
	if ok {
		t.Error("shell_exec should not trigger a path check")
	}
}

func TestExtractCheckPath_AbsentParam(t *testing.T) {
	_, ok := extractCheckPath("file_read", map[string]any{})
	if ok {
		t.Error("absent path param should return needsCheck=false")
	}
}

func TestExtractCheckPath_GitBlamePath(t *testing.T) {
	path, ok := extractCheckPath("git_blame", map[string]any{"file_path": "/home/user/project/main.go"})
	if !ok {
		t.Fatal("expected needsCheck=true for git_blame")
	}
	if path != "/home/user/project/main.go" {
		t.Errorf("unexpected path: %q", path)
	}
}

func TestExtractCheckPath_GitBlameWithRepoPath(t *testing.T) {
	path, ok := extractCheckPath("git_blame", map[string]any{
		"file_path": "main.go",
		"repo_path": "/home/user/project",
	})
	if !ok {
		t.Fatal("expected needsCheck=true for git_blame with repo_path")
	}
	want := filepath.Join("/home/user/project", "main.go")
	if path != want {
		t.Errorf("expected %q, got %q", want, path)
	}
}

func TestExtractCheckPath_GlobWithBaseDir(t *testing.T) {
	path, ok := extractCheckPath("file_glob", map[string]any{
		"pattern":  "*.go",
		"base_dir": "/home/user/project",
	})
	if !ok {
		t.Fatal("expected needsCheck=true for file_glob with base_dir")
	}
	if path != "/home/user/project" {
		t.Errorf("expected %q, got %q", "/home/user/project", path)
	}
}

func TestExtractCheckPath_GlobStaticPrefix(t *testing.T) {
	path, ok := extractCheckPath("file_glob", map[string]any{
		"pattern": "/home/user/project/*.go",
	})
	if !ok {
		t.Fatal("expected needsCheck=true for file_glob")
	}
	if path != "/home/user/project" {
		t.Errorf("expected %q, got %q", "/home/user/project", path)
	}
}

func TestExtractCheckPath_GlobBareWildcard(t *testing.T) {
	path, ok := extractCheckPath("file_glob", map[string]any{"pattern": "*.go"})
	if !ok {
		t.Fatal("expected needsCheck=true for bare wildcard glob")
	}
	if path != "." {
		t.Errorf("expected %q for bare wildcard, got %q", ".", path)
	}
}

func TestExtractCheckPath_GlobNoWildcard(t *testing.T) {
	path, ok := extractCheckPath("file_glob", map[string]any{"pattern": "/home/user/project/main.go"})
	if !ok {
		t.Fatal("expected needsCheck=true for glob without wildcard")
	}
	if !strings.HasSuffix(path, "main.go") {
		t.Errorf("expected path ending in main.go, got %q", path)
	}
}

// ── Duplicate root deduplication ──────────────────────────────────────────────

func TestPathPolicy_Allow_DeduplicatesRoots(t *testing.T) {
	pp := NewPathPolicy()
	root := t.TempDir()

	_ = pp.Allow(root)
	_ = pp.Allow(root) // duplicate
	_ = pp.Allow(root) // duplicate

	pp.mu.RLock()
	n := len(pp.roots)
	pp.mu.RUnlock()

	if n != 1 {
		t.Errorf("expected 1 root after 3 duplicate Allow calls, got %d", n)
	}
}

// ── Concurrent safety ─────────────────────────────────────────────────────────

func TestPathPolicy_ConcurrentAllowAndIsAllowed(t *testing.T) {
	pp := NewPathPolicy()
	roots := make([]string, 5)
	for i := range roots {
		roots[i] = t.TempDir()
	}

	var wg sync.WaitGroup

	// Writers: add roots concurrently
	for _, r := range roots {
		wg.Go(func() { _ = pp.Allow(r) })
	}

	// Readers: call IsAllowed concurrently while roots are being added
	for _, r := range roots {
		wg.Go(func() { _ = pp.IsAllowed(filepath.Join(r, "file.go")) })
	}

	wg.Wait()

	// After all goroutines finish every root must be present.
	for _, r := range roots {
		if !pp.IsAllowed(filepath.Join(r, "final.go")) {
			t.Errorf("root %q should be allowed after concurrent Add", r)
		}
	}
}
