package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestStaticRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{"index.html": "<html>app</html>", "sw.js": "worker", "assets/main-abc.js": "bundle"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	router := chi.NewRouter()
	registerStaticRoutes(router, dir)
	for _, tc := range []struct {
		method, path string
		status       int
		noCache      bool
	}{
		{"GET", "/", 200, true}, {"HEAD", "/", 200, true}, {"GET", "/settings", 200, true},
		{"GET", "/sw.js", 200, true}, {"GET", "/assets/missing", 404, false},
		{"HEAD", "/assets/main-abc.js", 200, false}, {"GET", "/assets/main-abc.js", 200, false},
		{"GET", "/missing.png", 404, false},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d", w.Code)
			}
			if tc.noCache && w.Header().Get("Cache-Control") != "no-cache" {
				t.Fatalf("cache=%q", w.Header().Get("Cache-Control"))
			}
			if !tc.noCache && tc.status == 200 && w.Header().Get("Cache-Control") != "" {
				t.Fatalf("asset caching changed")
			}
			if tc.method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatal("HEAD returned body")
			}
		})
	}
}
