package routes

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/coah80/yoink/internal/config"
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
