package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFeinoDir_CreatesDirectory(t *testing.T) {
	// Point HOME at a temp directory so we don't touch the real ~/.feino.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir, err := feinoDir()
	if err != nil {
		t.Fatalf("feinoDir: unexpected error: %v", err)
	}

	want := filepath.Join(tmp, ".feino")
	if dir != want {
		t.Errorf("feinoDir() = %q, want %q", dir, want)
	}

	if info, err := os.Stat(dir); err != nil {
		t.Errorf("directory not created: %v", err)
	} else if !info.IsDir() {
		t.Error("path exists but is not a directory")
	}
}

func TestFeinoDir_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Calling twice must not error on the second call.
	if _, err := feinoDir(); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := feinoDir(); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

func TestOpenLogFile_CreatesFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	f, err := openLogFile()
	if err != nil {
		t.Fatalf("openLogFile: unexpected error: %v", err)
	}
	defer func() { _ = f.Close() }()

	want := filepath.Join(tmp, ".feino", "feino.log")
	if f.Name() != want {
		t.Errorf("log file path = %q, want %q", f.Name(), want)
	}
}
