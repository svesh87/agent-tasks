package webui

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandler(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX"), 0o644)
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("JS"), 0o644)
	h := Handler(dir)
	for path, want := range map[string]string{
		"/":                       "INDEX",
		"/t/T-1/d/plan":           "INDEX",
		"/assets/app.js":          "JS",
		"/t/T-1/d/assets/app.js":  "JS", // relative asset URL on a deep link
		"/t/T-1/d/plan/diff/1..2": "INDEX",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		body, _ := io.ReadAll(rec.Body)
		if string(body) != want {
			t.Errorf("%s: got %q, want %q", path, body, want)
		}
	}
	rec := httptest.NewRecorder()
	Handler("").ServeHTTP(rec, httptest.NewRequest("GET", "/t/T-1", nil))
	if rec.Code != 200 {
		t.Errorf("embedded placeholder: %d", rec.Code)
	}
}
