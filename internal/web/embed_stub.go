//go:build !web

package web

import "io/fs"

// EmbeddedFS returns an empty filesystem when the binary was compiled
// without the "web" build tag (i.e. without running `make build`).
// The --web flag will show an advisory message in this case.
func EmbeddedFS() fs.FS { return emptyFS{} }

type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }
