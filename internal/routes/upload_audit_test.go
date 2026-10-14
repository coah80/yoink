package routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
	"github.com/go-chi/chi/v5"
)

func TestUploadInitRejectsNegativeChunks(t *testing.T) {
	w := httptest.NewRecorder()
	handleUploadInit(w, httptest.NewRequest(http.MethodPost, "/api/upload/init", bytes.NewBufferString(`{"fileName":"video.mp4","fileSize":4,"totalChunks":-1}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestUploadAssemblyVerifiesSizeAndPreservesChunksOnFailure(t *testing.T) {
	previous := config.TempDirs["upload"]
	config.TempDirs["upload"] = t.TempDir()
	t.Cleanup(func() { config.TempDirs["upload"] = previous })
	for _, size := range []int64{3, 4, 5} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			id := fmt.Sprintf("audit-upload-%d", size)
			chunkPath := filepath.Join(config.TempDirs["upload"], "chunk-"+id+"-0000")
			if err := os.WriteFile(chunkPath, []byte("data"), 0600); err != nil {
				t.Fatal(err)
			}
			services.Global.SetChunkedUpload(id, &services.ChunkedUpload{FileName: "video.mp4", FileSize: size, TotalChunks: 1, ReceivedChunks: map[int]bool{0: true}})
			t.Cleanup(func() { services.Global.DeleteChunkedUpload(id) })
			router := chi.NewRouter()
			ConvertRoutes(router)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/upload/complete/"+id, nil))
			if size != 4 {
				if w.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400", w.Code)
				}
				if _, err := os.Stat(chunkPath); err != nil {
					t.Fatalf("lost retryable chunk: %v", err)
				}
				return
			}
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", w.Code, w.Body)
			}
			var result struct {
				FilePath string `json:"filePath"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { services.Global.DeleteFileRef(result.FilePath) })
			data, err := os.ReadFile(resolveFilePath(result.FilePath))
			if err != nil || string(data) != "data" {
				t.Fatalf("assembled data = %q, err = %v", data, err)
			}
		})
	}
}
