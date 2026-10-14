package routes

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/coah80/yoink/internal/services"
	"github.com/go-chi/chi/v5"
)

func TestBotDownloadHeadDoesNotConsumeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mp4")
	if err := os.WriteFile(path, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	const token = "head-test"
	data := &services.BotDownload{FilePath: path, FileName: "video.mp4", MimeType: "video/mp4"}
	services.Global.SetBotDownload(token, data)
	t.Cleanup(func() { services.Global.DeleteBotDownload(token) })
	r := chi.NewRouter()
	BotRoutes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("HEAD", "/api/bot/download/"+token, nil))
	if w.Code != 200 || w.Header().Get("Content-Length") != "5" || w.Body.Len() != 0 {
		t.Fatalf("unexpected HEAD response: %d, headers %v, body %q", w.Code, w.Header(), w.Body.String())
	}
	if data.Downloaded || services.Global.GetBotDownload(token) == nil {
		t.Fatal("HEAD consumed download token")
	}
}
