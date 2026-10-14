package routes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
	"github.com/go-chi/chi/v5"
)

func auditMediaFixture(t *testing.T, kind string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	dir := t.TempDir()
	oldDir := config.TempDirs[kind]
	oldTempDir := config.TempDir
	config.TempDir = dir
	config.TempDirs[kind] = dir
	jobID := "audit-" + kind + "-" + t.Name()
	input := filepath.Join(dir, "input.mp4")
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "2", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", input)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v: %s", err, output)
	}
	services.Global.SetAsyncJob(jobID, &services.AsyncJob{Status: "processing", CreatedAt: time.Now()})
	t.Cleanup(func() {
		services.Global.ReleaseJob(jobID)
		services.Global.DeleteAsyncJob(jobID)
		services.Global.RemoveCompletedAsyncJob(jobID)
		config.TempDirs[kind] = oldDir
		config.TempDir = oldTempDir
	})
	return input, jobID
}

func TestAuditSingleSegmentConversion(t *testing.T) {
	input, id := auditMediaFixture(t, "convert")
	err := handleConvertAsync(input, "input.mp4", "mp4", "", "medium", "always", "", "", "192", "", nil, nil, nil, nil, []convertSegment{{Start: 0.5, End: 1.5}}, id)
	if err != nil {
		t.Fatal(err)
	}
	job := services.Global.GetAsyncJob(id)
	path, _, _, status := job.GetDownloadInfo()
	if status != "complete" {
		t.Fatalf("job failed: %+v", job)
	}
	duration := probeDuration(path)
	if duration < 0.95 || duration > 1.15 {
		t.Fatalf("single segment duration = %f, want approximately 1 second", duration)
	}
}

func TestAuditAsyncCompressProbesMissingDuration(t *testing.T) {
	input, id := auditMediaFixture(t, "compress")
	if err := handleCompressAsync(input, "input.mp4", "", "1", "0", "quality", "medium", "fast", "none", false, id); err != nil {
		t.Fatal(err)
	}
	job := services.Global.GetAsyncJob(id)
	path, _, _, status := job.GetDownloadInfo()
	if status != "complete" {
		_, _, _, msg, _ := job.GetStatus()
		t.Fatalf("job status = %s: %s", status, msg)
	}
	if d := probeDuration(path); d < 1.9 || d > 2.2 {
		t.Fatalf("compressed duration = %f", d)
	}
}

func TestAuditCropOutsideVideoFails(t *testing.T) {
	input, id := auditMediaFixture(t, "convert")
	x, y, w, h := 150, 0, 100, 100
	_ = handleConvertAsync(input, "input.mp4", "mp4", "", "medium", "always", "", "", "192", "", &x, &y, &w, &h, nil, id)
	status, _, _, msg, _ := services.Global.GetAsyncJob(id).GetStatus()
	if status != "error" || msg == "" {
		t.Fatalf("out of bounds crop status = %s, error = %q", status, msg)
	}
}

func TestAuditCancelledTranscribePreservesNewJobSlot(t *testing.T) {
	input, id := auditMediaFixture(t, "transcribe")
	dir := filepath.Dir(input)
	started := filepath.Join(dir, "started")
	resume := filepath.Join(dir, "resume")
	script := "#!/bin/sh\ntrap 'while [ ! -f \"" + resume + "\" ]; do /bin/sleep 0.01; done; exit 1' TERM\n/usr/bin/touch \"" + started + "\"\nwhile :; do /bin/sleep 0.01; done\n"
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = handleTranscribeAsync(input, "input.mp4", transcribeOpts{OutputMode: "text", Model: "base", SubtitleFormat: "srt", CaptionSize: 72}, id)
	}()
	t.Cleanup(func() {
		_ = os.WriteFile(resume, nil, 0600)
		if p := services.Global.GetProcess(id); p != nil {
			p.KillProcess()
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transcription extraction did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	router := chi.NewRouter()
	CoreRoutes(router)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/cancel/"+id, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("cancel status = %d", w.Code)
	}
	newID := id + "-next"
	if check := services.Global.CanStartJob("transcribe"); !check.OK {
		t.Fatalf("cancel did not release slot: %s", check.Reason)
	}
	services.Global.SetProcess(newID, &services.ProcessInfo{JobType: "transcribe"})
	t.Cleanup(func() { services.Global.ReleaseJob(newID) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/cancel/"+id, nil))
	if err := os.WriteFile(resume, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled transcription did not exit")
	}
	active := services.Global.GetQueueStatus()["active"].(map[string]int)
	if active["transcribe"] != 1 {
		t.Fatalf("new transcription lost its slot: active = %d", active["transcribe"])
	}
}
