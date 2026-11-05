// Package webui serves the built React app (web/ → internal/webui/dist)
// embedded in the binary, with SPA fallback to index.html.
package webui

import (
	"bytes"
	"embed"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// dist always contains at least .gitkeep, so this compiles on a fresh clone
// before the UI has been built.
//
//go:embed all:dist
var embedded embed.FS

// Explicit content types: on Windows, mime.TypeByExtension consults the
// registry, which can map .js to text/plain and break module scripts.
var contentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".map":         "application/json",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".ico":         "image/x-icon",
	".webp":        "image/webp",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".txt":         "text/plain; charset=utf-8",
	".webmanifest": "application/manifest+json",
}

// New returns a Handler serving the embedded UI.
func New() *Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err) // "dist" is guaranteed by the embed directive
	}
	return newHandler(sub)
}

// Handler serves static UI files with SPA fallback.
type Handler struct {
	files   fs.FS
	index   []byte
	modTime time.Time
}

func newHandler(files fs.FS) *Handler {
	h := &Handler{files: files, modTime: time.Now()}
	if b, err := fs.ReadFile(files, "index.html"); err == nil {
		h.index = b
	} else {
		h.index = []byte(placeholder)
	}
	return h
}

// Built reports whether a real UI build is embedded.
func (h *Handler) Built() bool { return !bytes.Equal(h.index, []byte(placeholder)) }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if strings.HasPrefix(name, ".") || strings.Contains(name, "/.") {
		// Never serve dotfiles such as the .gitkeep placeholder.
		http.NotFound(w, r)
		return
	}
	if name != "" && name != "index.html" {
		if f, err := h.files.Open(name); err == nil {
			defer f.Close()
			if st, err := f.Stat(); err == nil && !st.IsDir() {
				if ct, ok := contentTypes[strings.ToLower(path.Ext(name))]; ok {
					w.Header().Set("Content-Type", ct)
				}
				if strings.HasPrefix(name, "assets/") {
					// Vite fingerprints everything under assets/.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				if rs, ok := f.(io.ReadSeeker); ok {
					http.ServeContent(w, r, name, h.modTime, rs)
					return
				}
			}
		}
		// A missing build asset (e.g. a stale hash) is a real 404. Other
		// dotted paths are client routes: /receivers/dev@test.local.
		if strings.HasPrefix(name, "assets/") || contentTypes[strings.ToLower(path.Ext(name))] != "" {
			http.NotFound(w, r)
			return
		}
	}

	// SPA fallback: every client-side route gets index.html.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, "index.html", h.modTime, bytes.NewReader(h.index))
}

const placeholder = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>Envry</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>body{font:15px/1.5 system-ui,sans-serif;background:#09090b;color:#fafafa;
display:grid;place-items:center;min-height:100vh;margin:0}main{max-width:32rem;padding:2rem}
code{background:#27272a;padding:.15rem .4rem;border-radius:.3rem}</style></head>
<body><main><h1>Envry is running</h1>
<p>The web UI was not built into this binary. Run <code>make build</code>
(or <code>make dev</code> and open the Vite URL). The REST API is available under <code>/api/v1</code>.</p>
</main></body></html>`
