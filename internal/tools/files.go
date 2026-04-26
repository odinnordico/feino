package tools

import (
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	// maxReadBytes caps file_read to avoid flooding the context window.
	maxReadBytes = 1 << 20 // 1 MB
)

// NewFileTools returns the file system tool suite: list_files, file_read,
// file_write, file_edit, and file_search.
func NewFileTools(logger *slog.Logger) []Tool {
	return []Tool{
		newFileListTool(logger),
		newFileReadTool(logger),
		newFileWriteTool(logger),
		newFileEditTool(logger),
		newFileSearchTool(logger),
	}
}

func newFileListTool(logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "The directory to list. Defaults to current directory.",
			},
			"recursive": map[string]any{
				"type":        "boolean",
				"description": "When true, lists all files recursively. Directories are shown with a trailing /. Defaults to false.",
			},
		},
	}

	return NewTool(
		"list_files",
		"List files and directories in a given path. Set recursive=true to walk the full tree.",
		schema,
		func(params map[string]any) ToolResult {
			path := getStringDefault(params, "path", ".")
			recursive := getBool(params, "recursive", false)
			logger.Info("listing files", "path", path, "recursive", recursive)

			if !recursive {
				entries, err := os.ReadDir(path)
				if err != nil {
					logger.Error("failed to list files", "path", path, "error", err)
					return NewToolResult("", fmt.Errorf("list_files: %w", err))
				}
				var names []string
				for _, e := range entries {
					name := e.Name()
					if e.IsDir() {
						name += "/"
					}
					names = append(names, name)
				}
				return NewToolResult(strings.Join(names, "\n"), nil)
			}

			// Recursive walk — return paths relative to the base directory.
			var lines []string
			err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return nil // skip unreadable entries
				}
				rel, relErr := filepath.Rel(path, p)
				if relErr != nil || rel == "." {
					return nil
				}
				if d.IsDir() {
					rel += "/"
				}
				lines = append(lines, rel)
				return nil
			})
			if err != nil {
				return NewToolResult("", fmt.Errorf("list_files: %w", err))
			}
			return NewToolResult(strings.Join(lines, "\n"), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

func newFileReadTool(logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path of the file to read.",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "First line to return (1-based). Defaults to 1 (start of file).",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of lines to return. 0 means all remaining lines. Defaults to 0.",
			},
		},
		"required": []string{"path"},
	}

	return NewTool(
		"file_read",
		"Read a file and return its contents with 1-based line numbers prefixed (format: N\\tline). Use offset and limit to page through large files. Capped at 1 MB.",
		schema,
		func(params map[string]any) ToolResult {
			path, ok := getString(params, "path")
			if !ok || path == "" {
				return NewToolResult("", fmt.Errorf("file_read: 'path' parameter is required"))
			}

			// offset is 1-based in the schema (more natural for models), converted
			// to 0-based internally.
			offset := max(getInt(params, "offset", 1), 1)
			offset-- // convert to 0-based
			limit := getInt(params, "limit", 0)

			logger.Debug("reading file", "path", path, "offset", offset+1, "limit", limit)

			f, err := os.Open(path)
			if err != nil {
				logger.Error("failed to open file", "path", path, "error", err)
				return NewToolResult("", fmt.Errorf("file_read: %w", err))
			}
			defer func() { _ = f.Close() }()

			// Read up to maxReadBytes to avoid flooding the context window.
			// Use LimitReader+1 to detect truncation without a second stat call.
			buf, err := io.ReadAll(io.LimitReader(f, maxReadBytes+1))
			if err != nil {
				return NewToolResult("", fmt.Errorf("file_read: %w", err))
			}
			truncated := len(buf) > maxReadBytes
			if truncated {
				buf = buf[:maxReadBytes]
			}

			// Split into lines preserving the original content.
			raw := strings.TrimRight(string(buf), "\n")
			allLines := strings.Split(raw, "\n")

			// Apply offset (0-based) and limit.
			if offset >= len(allLines) {
				return NewToolResult("", fmt.Errorf("file_read: offset %d exceeds file length %d", offset+1, len(allLines)))
			}
			selected := allLines[offset:]
			if limit > 0 && limit < len(selected) {
				selected = selected[:limit]
			}

			// Prefix each line with its 1-based line number.
			var sb strings.Builder
			for i, line := range selected {
				fmt.Fprintf(&sb, "%d\t%s\n", offset+i+1, line)
			}

			result := strings.TrimRight(sb.String(), "\n")
			if truncated {
				result += fmt.Sprintf("\n[truncated at %d MB — use offset/limit to read further]", maxReadBytes>>20)
			}
			return NewToolResult(result, nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

func newFileWriteTool(logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path of the file to write. Parent directories are created automatically.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "Content to write to the file. Overwrites any existing content.",
			},
		},
		"required": []string{"path", "content"},
	}

	return NewTool(
		"file_write",
		"Write content to a file atomically, creating it and any missing parent directories. Overwrites existing files.",
		schema,
		func(params map[string]any) ToolResult {
			path, ok := getString(params, "path")
			if !ok || path == "" {
				return NewToolResult("", fmt.Errorf("file_write: 'path' parameter is required"))
			}
			content, ok := getString(params, "content")
			if !ok {
				return NewToolResult("", fmt.Errorf("file_write: 'content' parameter is required"))
			}

			logger.Info("writing file", "path", path, "bytes", len(content))

			if dir := filepath.Dir(path); dir != "." {
				if err := os.MkdirAll(dir, 0755); err != nil {
					logger.Error("failed to create directories", "path", dir, "error", err)
					return NewToolResult("", fmt.Errorf("file_write: creating directories: %w", err))
				}
			}

			if err := atomicWrite(path, []byte(content), 0644); err != nil {
				logger.Error("failed to write file", "path", path, "error", err)
				return NewToolResult("", fmt.Errorf("file_write: %w", err))
			}
			return NewToolResult(fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}

func newFileEditTool(logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path of the file to edit.",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The exact string to find. Must match exactly including whitespace and indentation.",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The replacement string.",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "When true, replaces every occurrence. When false (default), the call fails if old_string appears more than once — add more context to make it unique.",
			},
		},
		"required": []string{"path", "old_string", "new_string"},
	}

	return NewTool(
		"file_edit",
		"Replace old_string with new_string in a file. Fails if old_string is not found or (by default) appears more than once. Set replace_all=true to replace every occurrence.",
		schema,
		func(params map[string]any) ToolResult {
			path, ok := getString(params, "path")
			if !ok || path == "" {
				return NewToolResult("", fmt.Errorf("file_edit: 'path' parameter is required"))
			}
			oldStr, ok := getString(params, "old_string")
			if !ok {
				return NewToolResult("", fmt.Errorf("file_edit: 'old_string' parameter is required"))
			}
			newStr, ok := getString(params, "new_string")
			if !ok {
				return NewToolResult("", fmt.Errorf("file_edit: 'new_string' parameter is required"))
			}
			replaceAll := getBool(params, "replace_all", false)

			logger.Info("editing file", "path", path, "replace_all", replaceAll)

			data, err := os.ReadFile(path)
			if err != nil {
				logger.Error("failed to read file for edit", "path", path, "error", err)
				return NewToolResult("", fmt.Errorf("file_edit: %w", err))
			}

			original := string(data)
			count := strings.Count(original, oldStr)
			if count == 0 {
				return NewToolResult("", fmt.Errorf("file_edit: %q not found in %s — ensure indentation and line endings match exactly", oldStr, path))
			}
			if count > 1 && !replaceAll {
				return NewToolResult("", fmt.Errorf("file_edit: %q appears %d times in %s — add more surrounding context to make it unique, or set replace_all=true", oldStr, count, path))
			}

			updated := strings.ReplaceAll(original, oldStr, newStr)
			if err := atomicWrite(path, []byte(updated), 0644); err != nil {
				logger.Error("failed to write file after edit", "path", path, "error", err)
				return NewToolResult("", fmt.Errorf("file_edit: %w", err))
			}

			logger.Info("file_edit successful", "path", path, "replacements", count)
			return NewToolResult(fmt.Sprintf("replaced %d occurrence(s) in %s", count, path), nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}

func newFileSearchTool(logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern to match files against. Supports ** for recursive matching (e.g. **/*.go, src/**/*.ts).",
			},
			"base_dir": map[string]any{
				"type":        "string",
				"description": "Base directory to search in. Defaults to current directory.",
			},
		},
		"required": []string{"pattern"},
	}

	return NewTool(
		"file_search",
		"Find files matching a glob pattern. Supports ** for recursive directory matching (e.g. **/*.go). Returns newline-separated paths relative to base_dir.",
		schema,
		func(params map[string]any) ToolResult {
			pattern, ok := getString(params, "pattern")
			if !ok || pattern == "" {
				return NewToolResult("", fmt.Errorf("file_search: 'pattern' parameter is required"))
			}
			baseDir := getStringDefault(params, "base_dir", ".")

			logger.Debug("searching files", "pattern", pattern, "base_dir", baseDir)

			fsys := os.DirFS(baseDir)
			matches, err := doublestar.Glob(fsys, pattern)
			if err != nil {
				logger.Error("glob search failed", "pattern", pattern, "error", err)
				return NewToolResult("", fmt.Errorf("file_search: invalid pattern: %w", err))
			}
			return NewToolResult(strings.Join(matches, "\n"), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// atomicWrite writes data to path via a temp file + rename to avoid partial
// writes on crash or signal. The temp file is created in the same directory as
// path so that the rename is always on the same filesystem.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".feino-write-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
