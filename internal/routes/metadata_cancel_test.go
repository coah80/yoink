package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMetadataCancellationRunsWrapperCleanup(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "yt-dlp")
	ready := filepath.Join(dir, "ready")
	cleaned := filepath.Join(dir, "cleaned")
	t.Setenv("YTDLP_BIN", bin)
	t.Setenv("AUDIT_READY", ready)
	t.Setenv("AUDIT_CLEANED", cleaned)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ntrap 'touch \"$AUDIT_CLEANED\"; exit 1' TERM\ntouch \"$AUDIT_READY\"\nwhile :; do sleep 0.1; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleMetadata(w, httptest.NewRequest(http.MethodGet, "/api/metadata?url=https://www.youtube.com/watch?v=audit", nil).WithContext(ctx))
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("wrapper did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("metadata cancellation did not complete")
	}
	if _, err := os.Stat(cleaned); err != nil {
		t.Fatalf("wrapper cleanup did not run: %v", err)
	}
	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("status=%d body=%s", w.Code, w.Body)
	}
}
