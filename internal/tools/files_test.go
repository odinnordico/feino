package tools

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileRead(t *testing.T) {
	tool := newFileReadTool(slog.Default())

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	content := "line1\nline2\nline3\nline4\nline5"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		params      map[string]any
		wantContent string
		wantErr     bool
	}{
		{
			name:        "full file has line numbers",
			params:      map[string]any{"path": path},
			wantContent: "1\tline1",
		},
		{
			name:        "last line has correct number",
			params:      map[string]any{"path": path},
			wantContent: "5\tline5",
		},
		{
			name:        "offset 3 starts at line 3",
			params:      map[string]any{"path": path, "offset": 3},
			wantContent: "3\tline3",
		},
		{
			name:        "offset 3 does not include line 2",
			params:      map[string]any{"path": path, "offset": 3},
			wantContent: "3\tline3",
		},
		{
			name:        "limit 2 returns only 2 lines",
			params:      map[string]any{"path": path, "limit": 2},
			wantContent: "1\tline1",
		},
		{
			name:        "offset 2 limit 2 returns lines 2-3",
			params:      map[string]any{"path": path, "offset": 2, "limit": 2},
			wantContent: "2\tline2",
		},
		{
			name:    "missing path param",
			params:  map[string]any{},
			wantErr: true,
		},
		{
			name:    "non-existent file",
			params:  map[string]any{"path": filepath.Join(dir, "no-such-file.txt")},
			wantErr: true,
		},
		{
			name:    "offset beyond file length returns error",
			params:  map[string]any{"path": path, "offset": 100},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tool.Run(tc.params)

			if tc.wantErr && result.GetError() == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && result.GetError() != nil {
				t.Errorf("unexpected error: %v", result.GetError())
			}
			if tc.wantContent != "" {
				got, _ := result.GetContent().(string)
				if !strings.Contains(got, tc.wantContent) {
					t.Errorf("expected content to contain %q, got %q", tc.wantContent, got)
				}
			}
		})
	}
}

func TestFileWrite(t *testing.T) {
	tool := newFileWriteTool(slog.Default())
	dir := t.TempDir()

	tests := []struct {
		name    string
		params  map[string]any
		check   func(t *testing.T)
		wantErr bool
	}{
		{
			name:   "create new file",
			params: map[string]any{"path": filepath.Join(dir, "new.txt"), "content": "hello"},
			check: func(t *testing.T) {
				data, _ := os.ReadFile(filepath.Join(dir, "new.txt"))
				if string(data) != "hello" {
					t.Errorf("expected %q, got %q", "hello", data)
				}
			},
		},
		{
			name: "overwrite existing file",
			params: func() map[string]any {
				_ = os.WriteFile(filepath.Join(dir, "overwrite.txt"), []byte("first"), 0644)
				return map[string]any{"path": filepath.Join(dir, "overwrite.txt"), "content": "second"}
			}(),
			check: func(t *testing.T) {
				data, _ := os.ReadFile(filepath.Join(dir, "overwrite.txt"))
				if string(data) != "second" {
					t.Errorf("expected %q, got %q", "second", data)
				}
			},
		},
		{
			name:   "creates parent directories",
			params: map[string]any{"path": filepath.Join(dir, "sub", "dir", "file.txt"), "content": "nested"},
			check: func(t *testing.T) {
				data, _ := os.ReadFile(filepath.Join(dir, "sub", "dir", "file.txt"))
				if string(data) != "nested" {
					t.Errorf("expected %q, got %q", "nested", data)
				}
			},
		},
		{
			name:    "missing path param",
			params:  map[string]any{"content": "x"},
			wantErr: true,
		},
		{
			name:    "missing content param",
			params:  map[string]any{"path": filepath.Join(dir, "x.txt")},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tool.Run(tc.params)

			if tc.wantErr && result.GetError() == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && result.GetError() != nil {
				t.Errorf("unexpected error: %v", result.GetError())
			}
			if tc.check != nil && !tc.wantErr {
				tc.check(t)
			}
		})
	}
}

func TestFileEdit(t *testing.T) {
	tool := newFileEditTool(slog.Default())
	dir := t.TempDir()

	writeFile := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		return path
	}

	tests := []struct {
		name        string
		params      map[string]any
		wantContent string // substring in result content
		wantFile    string // expected full file content after edit
		wantErr     bool
	}{
		{
			name:        "single replacement",
			params:      map[string]any{"path": writeFile("single.txt", "hello world"), "old_string": "world", "new_string": "Go"},
			wantContent: "replaced 1",
			wantFile:    "hello Go",
		},
		{
			name:    "ambiguous match errors by default",
			params:  map[string]any{"path": writeFile("ambig.txt", "foo foo foo"), "old_string": "foo", "new_string": "bar"},
			wantErr: true,
			wantFile: "foo foo foo", // file must be unchanged
		},
		{
			name:        "replace_all=true replaces every occurrence",
			params:      map[string]any{"path": writeFile("multi.txt", "foo foo foo"), "old_string": "foo", "new_string": "bar", "replace_all": true},
			wantContent: "replaced 3",
			wantFile:    "bar bar bar",
		},
		{
			name:     "no match is error",
			params:   map[string]any{"path": writeFile("nomatch.txt", "hello"), "old_string": "xyz", "new_string": "abc"},
			wantErr:  true,
			wantFile: "hello",
		},
		{
			name:    "non-existent file",
			params:  map[string]any{"path": filepath.Join(dir, "ghost.txt"), "old_string": "a", "new_string": "b"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tool.Run(tc.params)

			if tc.wantErr && result.GetError() == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && result.GetError() != nil {
				t.Errorf("unexpected error: %v", result.GetError())
			}
			if tc.wantContent != "" {
				got, _ := result.GetContent().(string)
				if !strings.Contains(got, tc.wantContent) {
					t.Errorf("expected result to contain %q, got %q", tc.wantContent, got)
				}
			}
			if tc.wantFile != "" {
				path, _ := tc.params["path"].(string)
				data, _ := os.ReadFile(path)
				if string(data) != tc.wantFile {
					t.Errorf("expected file content %q, got %q", tc.wantFile, string(data))
				}
			}
		})
	}
}

func TestFileSearch(t *testing.T) {
	tool := newFileSearchTool(slog.Default())
	dir := t.TempDir()

	// Create a small tree: root/{a.txt, b.txt, sub/c.go, sub/d.go}
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0644)
	_ = os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	_ = os.WriteFile(filepath.Join(dir, "sub", "c.go"), []byte("x"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "sub", "d.go"), []byte("x"), 0644)

	tests := []struct {
		name        string
		params      map[string]any
		wantMatches []string
		wantMissing []string
		wantErr     bool
	}{
		{
			name:        "flat glob matches txt files",
			params:      map[string]any{"pattern": "*.txt", "base_dir": dir},
			wantMatches: []string{"a.txt", "b.txt"},
			wantMissing: []string{"c.go"},
		},
		{
			name:        "double-star finds go files in subdirectory",
			params:      map[string]any{"pattern": "**/*.go", "base_dir": dir},
			wantMatches: []string{"sub/c.go", "sub/d.go"},
			wantMissing: []string{"a.txt"},
		},
		{
			name:        "double-star finds all files",
			params:      map[string]any{"pattern": "**/*", "base_dir": dir},
			wantMatches: []string{"a.txt", "sub/c.go"},
		},
		{
			name:    "missing pattern param",
			params:  map[string]any{},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tool.Run(tc.params)

			if tc.wantErr && result.GetError() == nil {
				t.Errorf("expected error, got nil")
			}
			if !tc.wantErr && result.GetError() != nil {
				t.Errorf("unexpected error: %v", result.GetError())
			}
			got, _ := result.GetContent().(string)
			for _, want := range tc.wantMatches {
				if !strings.Contains(got, want) {
					t.Errorf("expected result to contain %q, got %q", want, got)
				}
			}
			for _, missing := range tc.wantMissing {
				if strings.Contains(got, missing) {
					t.Errorf("expected result NOT to contain %q, got %q", missing, got)
				}
			}
		})
	}
}

func TestListFiles(t *testing.T) {
	tool := newFileListTool(slog.Default())
	dir := t.TempDir()

	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0644)
	_ = os.MkdirAll(filepath.Join(dir, "sub"), 0755)
	_ = os.WriteFile(filepath.Join(dir, "sub", "b.txt"), []byte("x"), 0644)

	t.Run("flat listing shows direct children only", func(t *testing.T) {
		result := tool.Run(map[string]any{"path": dir})
		if result.GetError() != nil {
			t.Fatal(result.GetError())
		}
		got, _ := result.GetContent().(string)
		if !strings.Contains(got, "a.txt") {
			t.Errorf("expected a.txt in listing, got %q", got)
		}
		if !strings.Contains(got, "sub/") {
			t.Errorf("expected sub/ in listing, got %q", got)
		}
		if strings.Contains(got, "b.txt") {
			t.Errorf("expected b.txt NOT in flat listing, got %q", got)
		}
	})

	t.Run("recursive listing includes subdirectory files", func(t *testing.T) {
		result := tool.Run(map[string]any{"path": dir, "recursive": true})
		if result.GetError() != nil {
			t.Fatal(result.GetError())
		}
		got, _ := result.GetContent().(string)
		if !strings.Contains(got, "sub/b.txt") {
			t.Errorf("expected sub/b.txt in recursive listing, got %q", got)
		}
	})
}
