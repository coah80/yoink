package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/coah80/yoink/internal/config"
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

func TestEnsureTempDirsPreservesExistingFiles(t *testing.T) {
	original := config.TempDirs
	t.Cleanup(func() {
		config.TempDirs = original
	})

	base := t.TempDir()
	downloadDir := filepath.Join(base, "downloads")
	convertDir := filepath.Join(base, "convert")
	config.TempDirs = map[string]string{
		"download": downloadDir,
		"convert":  convertDir,
	}
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(downloadDir, "partial-download.part")
	if err := os.WriteFile(marker, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	EnsureTempDirs()

	if data, err := os.ReadFile(marker); err != nil || string(data) != "partial" {
		t.Fatalf("partial download was not preserved: data=%q err=%v", data, err)
	}
	if info, err := os.Stat(convertDir); err != nil || !info.IsDir() {
		t.Fatalf("missing temp directory: info=%v err=%v", info, err)
	}
}
