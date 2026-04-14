package tools

import (
	"log/slog"
	"strings"
	"testing"
)

func TestShellExec(t *testing.T) {
	tool := newShellExecTool(slog.Default())

	tests := []struct {
		name        string
		params      map[string]any
		wantContent string // substring expected in content (empty means skip check)
		wantErr     bool
	}{
		{
			name:        "echo hello",
			params:      map[string]any{"command": "echo hello"},
			wantContent: "hello",
		},
		{
			name:    "failing command exit 1",
			params:  map[string]any{"command": "exit 1"},
			wantErr: true,
		},
		{
			name:        "stderr captured",
			params:      map[string]any{"command": "echo oops >&2; exit 2"},
			wantContent: "oops",
			wantErr:     true,
		},
		{
			name:    "missing command param",
			params:  map[string]any{},
			wantErr: true,
		},
		{
			name:    "empty command string",
			params:  map[string]any{"command": "   "},
			wantErr: true,
		},
		{
			name:        "timeout_seconds as float64 (JSON-decoded)",
			params:      map[string]any{"command": "echo fast", "timeout_seconds": float64(5)},
			wantContent: "fast",
		},
		{
			name:        "multiline output",
			params:      map[string]any{"command": "printf 'a\\nb\\nc\\n'"},
			wantContent: "a\nb\nc",
		},
		{
			name:        "working_dir changes execution directory",
			params:      map[string]any{"command": "pwd", "working_dir": "/tmp"},
			wantContent: "/tmp",
		},
		{
			name:    "invalid working_dir returns error",
			params:  map[string]any{"command": "pwd", "working_dir": "/nonexistent-feino-test-dir"},
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
				content, _ := result.GetContent().(string)
				if !strings.Contains(content, tc.wantContent) {
					t.Errorf("expected content to contain %q, got %q", tc.wantContent, content)
				}
			}
		})
	}
}
