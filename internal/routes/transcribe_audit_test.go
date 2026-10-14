package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coah80/yoink/internal/services"
)

func TestChunkedTranscribeHonorsRequestedModel(t *testing.T) {
	input := filepath.Join(t.TempDir(), "speech.wav")
	if err := os.WriteFile(input, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	services.Global.SetFileRef("audit-transcribe-ref", &services.FileRef{FilePath: input, CreatedAt: time.Now()})
	t.Cleanup(func() { services.Global.DeleteFileRef("audit-transcribe-ref") })
	w := httptest.NewRecorder()
	handleTranscribeChunked(w, httptest.NewRequest(http.MethodPost, "/api/transcribe-chunked", bytes.NewBufferString(`{"filePath":"audit-transcribe-ref","model":"invalid-model"}`)))
	var result struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.JobID == "" {
		t.Fatalf("missing job: %s", w.Body)
	}
	t.Cleanup(func() { services.Global.DeleteAsyncJob(result.JobID) })
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job := services.Global.GetAsyncJob(result.JobID)
		_, _, _, errMsg, _ := job.GetStatus()
		if strings.Contains(errMsg, "Invalid model") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("requested model was ignored")
}
