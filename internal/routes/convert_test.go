package routes

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
	"github.com/go-chi/chi/v5"
)

func TestAsyncJobRangeDownloadPreservesOutput(t *testing.T) {
	jobID := "async-range-download-test"
	outputPath := filepath.Join(t.TempDir(), jobID+"-converted.mp4")
	if err := os.WriteFile(outputPath, []byte("test media"), 0o600); err != nil {
		t.Fatal(err)
	}
	services.Global.SetAsyncJob(jobID, &services.AsyncJob{
		Status:         "complete",
		Progress:       100,
		CreatedAt:      time.Now(),
		OutputPath:     outputPath,
		OutputFilename: "video.mp4",
		MimeType:       "video/mp4",
	})
	t.Cleanup(func() {
		services.Global.DeleteAsyncJob(jobID)
		services.Global.RemoveCompletedAsyncJob(jobID)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/job/"+jobID+"/download", nil)
	req.Header.Set("Range", "bytes=0-0")
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("jobId", jobID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))
	response := httptest.NewRecorder()

	handleJobDownload(response, req)

	if response.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusPartialContent)
	}
	if response.Body.Len() != 1 {
		t.Fatalf("body bytes = %d, want 1", response.Body.Len())
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("range download removed output: %v", err)
	}
	if services.Global.GetAsyncJob(jobID) == nil {
		t.Fatal("range download removed job")
	}
}

func TestAsyncConvertRegistersProcess(t *testing.T) {
	previousCommand := asyncFFmpegCommand
	previousConvertDir := config.TempDirs["convert"]
	workDir := t.TempDir()
	config.TempDirs["convert"] = workDir

	started := make(chan struct{}, 1)
	done := make(chan struct{})
	asyncFFmpegCommand = func(string, ...string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=TestAsyncConvertHelperProcess")
		cmd.Env = append(os.Environ(), "GO_WANT_ASYNC_CONVERT_HELPER=1")
		started <- struct{}{}
		return cmd
	}

	jobID := "async-convert-process-test"
	inputPath := filepath.Join(workDir, "input.webm")
	if err := os.WriteFile(inputPath, []byte("test media"), 0o600); err != nil {
		t.Fatal(err)
	}
	services.Global.SetAsyncJob(jobID, &services.AsyncJob{Status: "processing", CreatedAt: time.Now()})

	t.Cleanup(func() {
		if p := services.Global.GetProcess(jobID); p != nil {
			p.KillProcess()
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		services.Global.ReleaseJob(jobID)
		services.Global.DeleteAsyncJob(jobID)
		config.TempDirs["convert"] = previousConvertDir
		asyncFFmpegCommand = previousCommand
	})

	go func() {
		_ = handleConvertAsync(inputPath, "input.webm", "mp4", "", "medium", "always",
			"", "", "128", "", nil, nil, nil, nil, nil, jobID)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("async conversion did not start ffmpeg")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		p := services.Global.GetProcess(jobID)
		helperStarted := p != nil && p.GetCmd() != nil
		if helperStarted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("async conversion did not launch ffmpeg")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if services.Global.GetProcess(jobID) == nil {
		t.Fatal("async conversion did not register its process")
	}
	active := services.Global.GetQueueStatus()["active"].(map[string]int)
	if active["convert"] != 1 {
		t.Fatalf("active convert jobs = %d, want 1", active["convert"])
	}
}

func TestAsyncConvertHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_ASYNC_CONVERT_HELPER") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

func TestFetchURLRegistersProcess(t *testing.T) {
	previousCommand := fetchURLCommand
	previousID := fetchURLID
	started := make(chan struct{}, 1)
	done := make(chan struct{})
	var commandMu sync.Mutex
	var helperArgs []string
	jobID := "fetch-url-process-test"

	fetchURLID = func() string { return jobID }
	fetchURLCommand = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestFetchURLHelperProcess")
		cmd.Env = append(os.Environ(), "GO_WANT_FETCH_URL_HELPER=1")
		commandMu.Lock()
		helperArgs = append([]string{}, args...)
		commandMu.Unlock()
		started <- struct{}{}
		return cmd
	}

	t.Cleanup(func() {
		if p := services.Global.GetProcess(jobID); p != nil {
			p.KillProcess()
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
		services.Global.ReleaseJob(jobID)
		fetchURLCommand = previousCommand
		fetchURLID = previousID
	})

	req := httptest.NewRequest(http.MethodPost, "/api/fetch-url", bytes.NewBufferString(`{"url":"https://www.youtube.com/playlist?list=test"}`))
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)
	response := httptest.NewRecorder()
	go func() {
		handleFetchURL(response, req)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("URL fetch did not create its command")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		p := services.Global.GetProcess(jobID)
		helperStarted := p != nil && p.GetCmd() != nil
		if helperStarted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("URL fetch did not launch its command")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if services.Global.GetProcess(jobID) == nil {
		t.Fatal("URL fetch did not register its process")
	}
	active := services.Global.GetQueueStatus()["active"].(map[string]int)
	if active["fetchUrl"] != 1 {
		t.Fatalf("active URL fetch jobs = %d, want 1", active["fetchUrl"])
	}
	hasSingleItemLimit := false
	for i := 0; i+1 < len(helperArgs); i++ {
		if helperArgs[i] == "--playlist-items" && helperArgs[i+1] == "1" {
			hasSingleItemLimit = true
			break
		}
	}
	if !hasSingleItemLimit {
		t.Fatalf("URL fetch args do not limit playlist input to one item: %q", helperArgs)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(7 * time.Second):
		t.Fatal("URL fetch did not stop after request cancellation")
	}
	if services.Global.GetProcess(jobID) != nil {
		t.Fatal("cancelled URL fetch retained process slot")
	}
}

func TestFetchURLHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_FETCH_URL_HELPER") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}
