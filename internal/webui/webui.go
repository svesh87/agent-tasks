// Package webui serves the single-page web UI: the built files embedded from
// dist/ (a placeholder unless the UI was built into it before go build), or a
// directory on disk. Unknown paths get index.html so that client-side routes
// such as /t/T-12/d/plan work on reload.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

//go:embed dist
var embedded embed.FS

// Handler serves the UI from dir, or from the embedded files when dir is "".
func Handler(dir string) http.Handler {
	var root fs.FS
	if dir != "" {
		root = os.DirFS(dir)
	} else {
		root, _ = fs.Sub(embedded, "dist")
	}
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		// A build with relative asset URLs (base './') asks for
		// /t/T-1/d/assets/x.js on deep links; serve /assets/x.js.
		if i := strings.LastIndex(p, "/assets/"); i >= 0 {
			if _, err := fs.Stat(root, p); err != nil {
				p = p[i+1:]
				r = r.Clone(r.Context())
				r.URL.Path = "/" + p
			}
		}
		if p != "" {
			if st, err := fs.Stat(root, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(root, "index.html")
		if err != nil {
			http.Error(w, "web UI not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}
