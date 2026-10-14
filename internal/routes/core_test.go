package routes

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
)

func TestPublicLimitsMatchUploadLimit(t *testing.T) {
	w := httptest.NewRecorder()
	handleLimits(w, httptest.NewRequest("GET", "/api/limits", nil))
	var body struct {
		MaxFileSize int64 `json:"maxFileSize"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.MaxFileSize != config.FileSizeLimit {
		t.Fatalf("advertised %d, enforced %d", body.MaxFileSize, config.FileSizeLimit)
	}
}

func TestAdminDrainNeedsToken(t *testing.T) {
	previous := config.AdminToken
	t.Cleanup(func() {
		config.AdminToken = previous
		services.Global.SetDraining(false)
	})
	send := func(auth string) int {
		r := httptest.NewRequest("POST", "/api/admin/drain", nil)
		r.RemoteAddr = "127.0.0.1:5555"
		r.Header.Set("True-Client-IP", "127.0.0.1")
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		handleAdminDrain(w, r)
		return w.Code
	}
	config.AdminToken = ""
	if code := send("Bearer anything"); code != 404 {
		t.Fatalf("no token configured: got %d, want 404", code)
	}
	config.AdminToken = "s3cret"
	if code := send(""); code != 401 {
		t.Fatalf("missing auth from loopback: got %d, want 401", code)
	}
	if code := send("Bearer wrong"); code != 401 {
		t.Fatalf("wrong token: got %d, want 401", code)
	}
	if services.Global.IsDraining() {
		t.Fatal("rejected request still drained the server")
	}
	if code := send("Bearer s3cret"); code != 200 || !services.Global.IsDraining() {
		t.Fatalf("right token: got %d, draining=%v", code, services.Global.IsDraining())
	}
}
