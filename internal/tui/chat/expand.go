package chat

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxAtRefFileSize = 20 * 1024 * 1024 // 20 MB
	maxTreeDepth     = 5
	maxTreeEntries   = 500
)

var errMaxEntries = errors.New("limit reached")

// atRefRe matches @word tokens used for file-path expansion.
var atRefRe = regexp.MustCompile(`@\S+`)

// dirSkip lists directory names that are never descended into during tree expansion.
var dirSkip = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	".hg": true, ".svn": true, "__pycache__": true,
}

// expandAtRefs replaces @path tokens in text with their contents:
//   - file < 20 MB  → <file> block with full content
//   - directory     → <directory> block with recursive tree listing
//   - file ≥ 20 MB or unresolvable → left unchanged
func (m Model) expandAtRefs(text string) string {
	var sb strings.Builder
	indices := atRefRe.FindAllStringIndex(text, -1)
	if indices == nil {
		return text
	}

	last := 0
	for _, idx := range indices {
		sb.WriteString(text[last:idx[0]])
		token := text[idx[0]:idx[1]]
		sb.WriteString(m.expandToken(token))
		last = idx[1]
	}
	sb.WriteString(text[last:])
	return sb.String()
}

func (m Model) expandToken(token string) string {
	relPath := strings.TrimPrefix(token, "@")

	// Resolve to absolute path; never join if relPath is already absolute.
	absPath := relPath
	if !filepath.IsAbs(relPath) && m.cfg.Context.WorkingDir != "" {
		absPath = filepath.Join(m.cfg.Context.WorkingDir, relPath)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return token // path does not exist
	}

	if info.IsDir() {
		return expandDirRef(absPath, relPath)
	}

	if info.Size() > maxAtRefFileSize {
		return token // file too large to embed
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return token
	}
	return fmt.Sprintf("\n<file path=%q>\n%s\n</file>", relPath, string(content))
}

// expandDirRef builds a recursive tree listing for a directory @-reference.
// Each entry shows its path relative to the referenced directory root and its
// absolute path, e.g.:
//
//	<directory rel="src" abs="/project/src">
//	  internal/ [rel: src/internal | abs: /project/src/internal]
//	    foo.go  [rel: src/internal/foo.go | abs: /project/src/internal/foo.go]
//	</directory>
func expandDirRef(absPath, relPath string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "\n<directory rel=%q abs=%q>\n", relPath, absPath)

	entryCount := 0
	err := filepath.WalkDir(absPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		entryRel, _ := filepath.Rel(absPath, path)
		if entryRel == "." {
			return nil // skip the root itself
		}
		if d.IsDir() && dirSkip[d.Name()] {
			return filepath.SkipDir
		}

		if entryCount >= maxTreeEntries {
			return errMaxEntries
		}
		entryCount++

		depth := strings.Count(entryRel, string(filepath.Separator))
		indent := strings.Repeat("  ", depth)
		name := d.Name()
		if d.IsDir() {
			name += "/"
		}

		displayRel := filepath.Join(relPath, entryRel)
		fmt.Fprintf(&sb, "%s%s [rel: %s | abs: %s]\n", indent, name, displayRel, path)

		if d.IsDir() && depth >= maxTreeDepth {
			return filepath.SkipDir
		}

		return nil
	})

	if errors.Is(err, errMaxEntries) {
		fmt.Fprintf(&sb, "  ... (truncated at %d entries)\n", maxTreeEntries)
	} else if err != nil {
		fmt.Fprintf(&sb, "(error reading directory: %v)\n", err)
	}
	sb.WriteString("</directory>")
	return sb.String()
}
