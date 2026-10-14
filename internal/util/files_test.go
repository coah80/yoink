package util

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coah80/yoink/internal/config"
)

func TestCleanupKeepsPromisedFiles(t *testing.T) {
	dir := t.TempDir()
	previous := config.TempDirs
	config.TempDirs = map[string]string{"playlist": dir}
	t.Cleanup(func() { config.TempDirs = previous })
	old := time.Now().Add(-time.Hour)
	for _, name := range []string{"ready.zip", "orphan.zip"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("zip"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	CleanupTempFiles(func(path string, _ time.Time) bool { return filepath.Base(path) == "ready.zip" })
	if _, err := os.Stat(filepath.Join(dir, "ready.zip")); err != nil {
		t.Fatalf("deleted a file for an unexpired link: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "orphan.zip")); !os.IsNotExist(err) {
		t.Fatal("orphan file was not cleaned up")
	}
}

func TestCleanupTempFilesPreservesProtectedRecovery(t *testing.T) {
	root := t.TempDir()
	downloadDir := filepath.Join(root, "downloads")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldTempDir := config.TempDir
	oldTempDirs := config.TempDirs
	t.Cleanup(func() {
		config.TempDir = oldTempDir
		config.TempDirs = oldTempDirs
	})
	config.TempDir = root
	config.TempDirs = map[string]string{"download": downloadDir}

	protected := filepath.Join(downloadDir, "ready-recovery.mp4")
	expired := filepath.Join(downloadDir, "expired.mp4")
	for _, path := range []string{protected, expired} {
		if err := os.WriteFile(path, []byte("media"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-config.FileRetention - time.Minute)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	CleanupTempFiles(nil, protected)

	if _, err := os.Stat(protected); err != nil {
		t.Fatalf("protected recovery file was removed: %v", err)
	}
	if _, err := os.Stat(expired); !os.IsNotExist(err) {
		t.Fatalf("expired unprotected file was not removed: %v", err)
	}
}
