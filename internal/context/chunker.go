package context

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
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

// TreeSitterParser bounds native CGO parsers for code semantics logic.
// It is safe for concurrent use.
type TreeSitterParser struct {
	mu       sync.Mutex
	logger   *slog.Logger
	goParser *sitter.Parser
}

// NewTreeSitterParser initializes grammar and language bindings for the AST.
func NewTreeSitterParser(logger *slog.Logger) *TreeSitterParser {
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())
	return &TreeSitterParser{
		logger:   logger,
		goParser: parser,
	}
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

// extractGoChunks handles AST-based extraction for Go files.
func (t *TreeSitterParser) extractGoChunks(ctx context.Context, sourceCode []byte, filePath string, skeletonOnly bool) ([]SemanticChunk, error) {
	t.mu.Lock()
	tree, err := t.goParser.ParseCtx(ctx, nil, sourceCode)
	t.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to parse golang AST: %w", err)
	}
	defer tree.Close()

	root := tree.RootNode()
	var chunks []SemanticChunk
	processedNodes := make(map[uintptr]struct{})

	queryStr := `
		(function_declaration) @function
		(method_declaration) @method
		(type_declaration) @type
	`
	query, err := sitter.NewQuery([]byte(queryStr), golang.GetLanguage())
	if err != nil {
		return nil, fmt.Errorf("failed to compile AST query: %w", err)
	}
	defer query.Close()

	qc := sitter.NewQueryCursor()
	defer qc.Close()

	qc.Exec(query, root)

	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			node := capture.Node
			if _, seen := processedNodes[node.ID()]; seen {
				continue
			}
			processedNodes[node.ID()] = struct{}{}

			nodeType := node.Type()
			startByte := node.StartByte()
			endByte := node.EndByte()

			// Docstring Capture
			docStart := startByte
			prev := node.PrevSibling()
			for prev != nil && prev.Type() == "comment" {
				docStart = prev.StartByte()
				prev = prev.PrevSibling()
			}

			// Content Handling (Skeleton vs Full)
			var content string
			if skeletonOnly && (nodeType == "function_declaration" || nodeType == "method_declaration") {
				block := node.ChildByFieldName("body")
				if block != nil {
					sigEnd := block.StartByte()
					content = string(sourceCode[docStart:sigEnd]) + " { ... }"
				} else {
					content = string(sourceCode[docStart:node.EndByte()])
				}
			} else {
				content = string(sourceCode[docStart:endByte])
			}

			// Identifier Extraction
			var identifier string
			switch nodeType {
			case "type_declaration":
				for i := range int(node.NamedChildCount()) {
					child := node.NamedChild(i)
					if child.Type() == "type_spec" {
						if nameNode := child.ChildByFieldName("name"); nameNode != nil {
							identifier = rget(sourceCode, nameNode)
						}
						break
					}
				}
			case "method_declaration", "function_declaration":
				if nameNode := node.ChildByFieldName("name"); nameNode != nil {
					identifier = rget(sourceCode, nameNode)
				}
			}

			// Large Chunk Management
			content = strings.TrimSpace(content)
			if len(content) > 12000 {
				t.logger.Warn("exceptionally large chunk detected",
					"identifier", identifier,
					"chars", len(content),
					"file", filePath,
				)
				content = fmt.Sprintf("// [FEINO WARNING]: Large semantic chunk (%d chars).\n\n%s", len(content), content)
			}

			chunks = append(chunks, SemanticChunk{
				Type:      nodeType,
				Name:      strings.TrimSpace(identifier),
				Content:   content,
				FilePath:  filePath,
				Language:  "go",
				StartLine: node.StartPoint().Row + 1,
				EndLine:   node.EndPoint().Row + 1,
			})
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

// rget is a helper to extract a string from source bytes based on node bounds.
func rget(source []byte, node *sitter.Node) string {
	start := node.StartByte()
	end := node.EndByte()
	if end > uint32(len(source)) || start >= end {
		return ""
	}
	return string(source[start:end])
}
