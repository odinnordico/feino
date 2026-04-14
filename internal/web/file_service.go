package web

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"

	feinov1 "github.com/odinnordico/feino/gen/feino/v1"
)

// fileService handles temporary file uploads and server-side file listing.
// Uploaded files are written to a temporary directory and referenced by an
// opaque token for the lifetime of the server process.
type fileService struct {
	// tmpDir is where uploaded files are stored.
	tmpDir string
	// workDir is the base for relative @path references.
	workDir string
	// mu guards tokens against concurrent HTTP handler access.
	mu sync.RWMutex
	// tokens maps upload token → absolute server path.
	tokens map[string]string
}

func newFileService(workDir string) (*fileService, error) {
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			workDir = "."
		}
	}
	dir, err := os.MkdirTemp("", "feino-uploads-*")
	if err != nil {
		return nil, fmt.Errorf("file_service: create temp dir: %w", err)
	}
	return &fileService{
		tmpDir:  dir,
		workDir: workDir,
		tokens:  make(map[string]string),
	}, nil
}

// Upload saves content to a temporary file and returns its token and path.
func (fs *fileService) Upload(filename string, content []byte) (*feinov1.UploadFileResponse, error) {
	safe := filepath.Base(filename)
	if safe == "" || safe == "." {
		return nil, fmt.Errorf("file_service: invalid filename %q", filename)
	}
	token := uuid.New().String()
	dest := filepath.Join(fs.tmpDir, token+"-"+safe)
	if err := os.WriteFile(dest, content, 0o600); err != nil {
		return nil, fmt.Errorf("file_service: write file: %w", err)
	}
	fs.mu.Lock()
	fs.tokens[token] = dest
	fs.mu.Unlock()
	return &feinov1.UploadFileResponse{
		Token:      token,
		ServerPath: dest,
		SizeBytes:  int64(len(content)),
	}, nil
}

// Resolve returns the server path for an upload token, or ("", false) if not found.
func (fs *fileService) Resolve(token string) (string, bool) {
	fs.mu.RLock()
	path, ok := fs.tokens[token]
	fs.mu.RUnlock()
	return path, ok
}

// Close removes the temporary upload directory.
func (fs *fileService) Close() {
	if err := os.RemoveAll(fs.tmpDir); err != nil {
		slog.Warn("file_service: cleanup temp dir", "path", fs.tmpDir, "error", err)
	}
}

// AbsPath returns the absolute path for a relative reference using the working dir.
func (fs *fileService) AbsPath(ref string) string {
	return filepath.Join(fs.workDir, ref)
}

// ListEntries returns directory entries for path. An empty path returns the
// working directory entries.
func listEntries(base, path string, dirsOnly bool) ([]*feinov1.FileEntry, string, error) {
	target := path
	if target == "" {
		var err error
		target, err = os.Getwd()
		if err != nil {
			return nil, "", fmt.Errorf("file_service: getwd: %w", err)
		}
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}

	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, "", fmt.Errorf("file_service: readdir %q: %w", target, err)
	}

	var out []*feinov1.FileEntry
	for _, e := range entries {
		if dirsOnly && !e.IsDir() {
			continue
		}
		info, err := e.Info()
		size := int64(0)
		if err == nil && !e.IsDir() {
			size = info.Size()
		}
		out = append(out, &feinov1.FileEntry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    size,
			RelPath: e.Name(),
		})
	}
	return out, target, nil
}
