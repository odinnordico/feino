package web

import (
	"io/fs"
	"net/http"
	"strings"
)

// spaHandler serves a React SPA from fsys.
// Any request path that does not match a real file is served index.html
// so that client-side routing (React Router) works on page refresh.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean the path and strip the leading slash.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Check whether the file actually exists in the embedded FS.
		f, err := fsys.Open(path)
		if err != nil {
			// Not found → serve the SPA shell so the JS router takes over.
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		_ = f.Close()

		fileServer.ServeHTTP(w, r)
	})
}
