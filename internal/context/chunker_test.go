package context

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestExtractSemanticChunks_SkeletonMode(t *testing.T) {
	source := `
package main

// Hello is a greeting function.
func Hello(name string) string {
	msg := fmt.Sprintf("Hello %s", name)
	return msg
}
`
	parser := NewTreeSitterParser(slog.Default())
	path := "test.go"

	// Test FULL extraction
	chunks, err := parser.ExtractSemanticChunks(context.Background(), []byte(source), path, false)
	if err != nil {
		t.Fatalf("full extraction failed: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk in full mode")
	}
	if !strings.Contains(chunks[0].Content, "fmt.Sprintf") {
		t.Errorf("Expected full content in regular mode")
	}

	// Test SKELETON extraction
	chunks, err = parser.ExtractSemanticChunks(context.Background(), []byte(source), path, true)
	if err != nil {
		t.Fatalf("skeleton extraction failed: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk in skeleton mode")
	}
	if strings.Contains(chunks[0].Content, "fmt.Sprintf") {
		t.Errorf("Expected body to be truncated in skeleton mode, got: %s", chunks[0].Content)
	}
	if !strings.Contains(chunks[0].Content, "{ ... }") {
		t.Errorf("Expected signature truncation marker { ... } in skeleton mode, got: %s", chunks[0].Content)
	}
}

func TestExtractSemanticChunks_LineBasedFallback(t *testing.T) {
	var source strings.Builder
	for range 120 {
		source.WriteString("line content\n")
	}

	parser := NewTreeSitterParser(slog.Default())
	path := "notes.txt"
	chunks, err := parser.ExtractSemanticChunks(context.Background(), []byte(source.String()), path, false)
	if err != nil {
		t.Fatalf("failed to extract chunks: %v", err)
	}

	if len(chunks) != 3 {
		t.Errorf("Expected 3 chunks for 120 lines (50 per chunk), got %d", len(chunks))
	}

	if chunks[0].Language != "plaintext" {
		t.Errorf("Expected plaintext language for fallback, got %s", chunks[0].Language)
	}

	if chunks[0].FilePath != path {
		t.Errorf("Expected FilePath %s, got %s", path, chunks[0].FilePath)
	}
}

func TestExtractSemanticChunks_GoMetadata(t *testing.T) {
	source := "package main\n\nfunc Foo() {}"
	parser := NewTreeSitterParser(slog.Default())
	path := "/home/user/project/main.go"

	chunks, err := parser.ExtractSemanticChunks(context.Background(), []byte(source), path, false)
	if err != nil {
		t.Fatalf("extraction failed: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}
	if chunks[0].FilePath != path {
		t.Errorf("Expected metadata FilePath %s, got %s", path, chunks[0].FilePath)
	}
	if chunks[0].Language != "go" {
		t.Errorf("Expected language go, got %s", chunks[0].Language)
	}
}
