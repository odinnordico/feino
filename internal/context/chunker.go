package context

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
)

// SemanticChunk holds the metadata and bounds of a high-precision AST slice.
type SemanticChunk struct {
	Type      string // "function_declaration", "type_declaration", "method_declaration", "line_chunk"
	Name      string // Parsed identifier name
	Content   string // The code/content of the chunk
	FilePath  string // Source file path
	Language  string // e.g. "go", "plaintext", "markdown"
	StartLine uint32
	EndLine   uint32
}

// TreeSitterParser provides code chunking using Go's built-in go/ast package.
// The name is kept for API compatibility; CGO is no longer required.
// It is safe for concurrent use.
type TreeSitterParser struct {
	mu     sync.Mutex
	logger *slog.Logger
}

// NewTreeSitterParser initializes the parser.
func NewTreeSitterParser(logger *slog.Logger) *TreeSitterParser {
	return &TreeSitterParser{logger: logger}
}

// ExtractSemanticChunks converts a raw codebase string into precise semantic elements.
// It detects the language based on the file extension and falls back to line-based chunking for unknown types.
func (t *TreeSitterParser) ExtractSemanticChunks(ctx context.Context, sourceCode []byte, filePath string, skeletonOnly bool) ([]SemanticChunk, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".go":
		return t.extractGoChunks(ctx, sourceCode, filePath, skeletonOnly)
	default:
		return t.extractLineChunks(sourceCode, filePath)
	}
}

// extractGoChunks handles AST-based extraction for Go files using go/ast.
func (t *TreeSitterParser) extractGoChunks(_ context.Context, sourceCode []byte, filePath string, skeletonOnly bool) ([]SemanticChunk, error) {
	fset := token.NewFileSet()

	t.mu.Lock()
	f, parseErr := parser.ParseFile(fset, filePath, sourceCode, parser.ParseComments)
	t.mu.Unlock()
	if parseErr != nil {
		t.logger.Warn("go/ast parse failed, falling back to line chunking", "file", filePath, "error", parseErr)
		return t.extractLineChunks(sourceCode, filePath)
	}

	srcLen := len(sourceCode)
	offset := func(pos token.Pos) int {
		o := fset.Position(pos).Offset
		if o > srcLen {
			return srcLen
		}
		return o
	}
	lineNum := func(pos token.Pos) uint32 {
		return uint32(fset.Position(pos).Line)
	}

	var chunks []SemanticChunk

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			nodeType := "function_declaration"
			if d.Recv != nil && len(d.Recv.List) > 0 {
				nodeType = "method_declaration"
			}

			startPos := d.Pos()
			if d.Doc != nil {
				startPos = d.Doc.Pos()
			}

			var content string
			if skeletonOnly && d.Body != nil {
				sigEnd := offset(d.Body.Lbrace)
				content = string(sourceCode[offset(startPos):sigEnd]) + "{ ... }"
			} else {
				content = string(sourceCode[offset(startPos):offset(d.End())])
			}
			content = strings.TrimSpace(content)

			if len(content) > 12000 {
				t.logger.Warn("exceptionally large chunk detected",
					"identifier", d.Name.Name,
					"chars", len(content),
					"file", filePath,
				)
				content = fmt.Sprintf("// [FEINO WARNING]: Large semantic chunk (%d chars).\n\n%s", len(content), content)
			}

			chunks = append(chunks, SemanticChunk{
				Type:      nodeType,
				Name:      d.Name.Name,
				Content:   content,
				FilePath:  filePath,
				Language:  "go",
				StartLine: lineNum(startPos),
				EndLine:   lineNum(d.End()),
			})

		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}

				// For ungrouped declarations the GenDecl's Doc is the type comment.
				// For grouped ones, prefer the individual TypeSpec's Doc if present.
				startPos := d.Pos()
				switch {
				case d.Doc != nil:
					startPos = d.Doc.Pos()
				case ts.Doc != nil:
					startPos = ts.Doc.Pos()
				}

				content := strings.TrimSpace(string(sourceCode[offset(startPos):offset(d.End())]))
				if len(content) > 12000 {
					t.logger.Warn("exceptionally large chunk detected",
						"identifier", ts.Name.Name,
						"chars", len(content),
						"file", filePath,
					)
					content = fmt.Sprintf("// [FEINO WARNING]: Large semantic chunk (%d chars).\n\n%s", len(content), content)
				}

				chunks = append(chunks, SemanticChunk{
					Type:      "type_declaration",
					Name:      ts.Name.Name,
					Content:   content,
					FilePath:  filePath,
					Language:  "go",
					StartLine: lineNum(startPos),
					EndLine:   lineNum(d.End()),
				})
			}
		}
	}

	return chunks, nil
}

// extractLineChunks provides a robust line-based fallback for non-AST languages.
func (t *TreeSitterParser) extractLineChunks(sourceCode []byte, filePath string) ([]SemanticChunk, error) {
	const linesPerChunk = 50
	var chunks []SemanticChunk

	scanner := bufio.NewScanner(bytes.NewReader(sourceCode))
	var currentLines []string
	startLine := uint32(1)
	lineCount := uint32(0)

	for scanner.Scan() {
		lineCount++
		currentLines = append(currentLines, scanner.Text())

		if len(currentLines) >= linesPerChunk {
			chunks = append(chunks, SemanticChunk{
				Type:      "line_chunk",
				Name:      fmt.Sprintf("%s:%d-%d", filepath.Base(filePath), startLine, lineCount),
				Content:   strings.Join(currentLines, "\n"),
				FilePath:  filePath,
				Language:  "plaintext",
				StartLine: startLine,
				EndLine:   lineCount,
			})
			currentLines = nil
			startLine = lineCount + 1
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading source for line chunking: %w", err)
	}

	if len(currentLines) > 0 {
		chunks = append(chunks, SemanticChunk{
			Type:      "line_chunk",
			Name:      fmt.Sprintf("%s:%d-%d", filepath.Base(filePath), startLine, lineCount),
			Content:   strings.Join(currentLines, "\n"),
			FilePath:  filePath,
			Language:  "plaintext",
			StartLine: startLine,
			EndLine:   lineCount,
		})
	}

	return chunks, nil
}
