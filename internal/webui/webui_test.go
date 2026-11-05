package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandler(t *testing.T) {
	h := newHandler(fstest.MapFS{
		"index.html":         {Data: []byte("<html>app</html>")},
		"favicon.svg":        {Data: []byte("<svg/>")},
		"assets/index-ab.js": {Data: []byte("console.log(1)")},
		".gitkeep":           {Data: nil},
	})
	if !h.Built() {
		t.Fatal("Built() = false with index.html present")
	}

	cases := []struct {
		path, wantBody, wantType, wantCache string
		wantCode                            int
	}{
		{"/", "<html>app</html>", "text/html; charset=utf-8", "no-cache", 200},
		{"/index.html", "<html>app</html>", "text/html; charset=utf-8", "no-cache", 200},
		{"/sent", "<html>app</html>", "text/html; charset=utf-8", "no-cache", 200},
		{"/message/01HXYZ", "<html>app</html>", "text/html; charset=utf-8", "no-cache", 200},
		{"/receivers/dev@test.local", "<html>app</html>", "text/html; charset=utf-8", "no-cache", 200},
		{"/missing.js", "", "", "", 404},
		{"/assets/index-ab.js", "console.log(1)", "text/javascript; charset=utf-8", "public, max-age=31536000, immutable", 200},
		{"/favicon.svg", "<svg/>", "image/svg+xml", "no-cache", 200},
		{"/assets/missing-123.js", "", "", "", 404},
		{"/.gitkeep", "", "", "", 404},
		// Traversal is cleaned to /etc/passwd, which is just a client route.
		{"/../../etc/passwd", "<html>app</html>", "text/html; charset=utf-8", "no-cache", 200},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
			if rec.Code != c.wantCode {
				t.Fatalf("code = %d, want %d", rec.Code, c.wantCode)
			}
			if c.wantCode != 200 {
				return
			}
			if got := rec.Body.String(); got != c.wantBody {
				t.Errorf("body = %q, want %q", got, c.wantBody)
			}
			if got := rec.Header().Get("Content-Type"); got != c.wantType {
				t.Errorf("Content-Type = %q, want %q", got, c.wantType)
			}
			if got := rec.Header().Get("Cache-Control"); got != c.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, c.wantCache)
			}
		})
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST code = %d, want 405", rec.Code)
	}
}

func TestPlaceholderWhenNotBuilt(t *testing.T) {
	h := newHandler(fstest.MapFS{".gitkeep": {}})
	if h.Built() {
		t.Fatal("Built() = true without index.html")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sent", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "not built") {
		t.Errorf("got %d %q", rec.Code, rec.Body.String())
	}
}
