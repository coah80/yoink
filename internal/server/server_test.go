package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSPARouteSharingStaticDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "updates"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("app shell"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := spaHandler(dir)
	for _, path := range []string{"/updates", "/updates/", "/settings"} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.String() != "app shell" {
			t.Fatalf("%s: got %d %q", path, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest("GET", "/assets/missing.js", nil))
	if w.Code != 404 {
		t.Fatalf("missing asset returned %d, want 404", w.Code)
	}
}
