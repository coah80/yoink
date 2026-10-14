package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coah80/yoink/internal/config"
)

func TestLimitsMatchesUploadFileSizeLimit(t *testing.T) {
	w := httptest.NewRecorder()
	handleLimits(w, httptest.NewRequest(http.MethodGet, "/api/limits", nil))
	var limits struct {
		MaxFileSize int64 `json:"maxFileSize"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &limits); err != nil {
		t.Fatal(err)
	}
	if limits.MaxFileSize != config.FileSizeLimit {
		t.Fatalf("advertised maxFileSize = %d, upload limit = %d", limits.MaxFileSize, config.FileSizeLimit)
	}
}
