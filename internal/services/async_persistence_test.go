package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coah80/yoink/internal/config"
)

func TestCompletedAsyncJobSurvivesStateReload(t *testing.T) {
	previousTempDir := config.TempDir
	previousConvertDir := config.TempDirs["convert"]
	previousCompressDir := config.TempDirs["compress"]
	root := t.TempDir()
	convertDir := filepath.Join(root, "convert")
	compressDir := filepath.Join(root, "compress")
	config.TempDir = root
	config.TempDirs["convert"] = convertDir
	config.TempDirs["compress"] = compressDir
	if err := os.MkdirAll(convertDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(compressDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		config.TempDir = previousTempDir
		config.TempDirs["convert"] = previousConvertDir
		config.TempDirs["compress"] = previousCompressDir
	})

	jobID := "completed-async-reload-test"
	outputPath := filepath.Join(convertDir, jobID+"-converted.mp4")
	if err := os.WriteFile(outputPath, []byte("test media"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := newState()
	state.SetAsyncJob(jobID, &AsyncJob{
		Status:         "complete",
		Progress:       100,
		CreatedAt:      time.Now(),
		OutputPath:     outputPath,
		OutputFilename: "video.mp4",
		MimeType:       "video/mp4",
	})
	if err := state.PersistCompletedAsyncJob(jobID); err != nil {
		t.Fatal(err)
	}

	reloaded := newState()
	count, err := reloaded.LoadCompletedAsyncJobs()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("loaded jobs = %d, want 1", count)
	}
	job := reloaded.GetAsyncJob(jobID)
	if job == nil {
		t.Fatal("completed async job was not restored")
	}
	path, filename, mimeType, status := job.GetDownloadInfo()
	if status != "complete" || path != outputPath || filename != "video.mp4" || mimeType != "video/mp4" {
		t.Fatalf("restored job = %q %q %q %q", status, path, filename, mimeType)
	}
}

func TestCompletedAsyncJobRestoresFromOutputFile(t *testing.T) {
	previousTempDir := config.TempDir
	previousConvertDir := config.TempDirs["convert"]
	previousCompressDir := config.TempDirs["compress"]
	root := t.TempDir()
	convertDir := filepath.Join(root, "convert")
	compressDir := filepath.Join(root, "compress")
	config.TempDir = root
	config.TempDirs["convert"] = convertDir
	config.TempDirs["compress"] = compressDir
	if err := os.MkdirAll(convertDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(compressDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		config.TempDir = previousTempDir
		config.TempDirs["convert"] = previousConvertDir
		config.TempDirs["compress"] = previousCompressDir
	})

	jobID := "completed-async-file-test"
	outputPath := filepath.Join(convertDir, jobID+"-converted.mp4")
	if err := os.WriteFile(outputPath, []byte("test media"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := newState()
	job := state.RestoreCompletedAsyncJob(jobID)
	if job == nil {
		t.Fatal("completed output file was not restored")
	}
	path, _, _, status := job.GetDownloadInfo()
	if status != "complete" || path != outputPath {
		t.Fatalf("restored job = %q %q", status, path)
	}
	if _, err := os.Stat(completedAsyncJobPath(jobID)); err != nil {
		t.Fatalf("restored job was not persisted: %v", err)
	}
}

func TestCompletedPlaylistRestoresDownloadToken(t *testing.T) {
	previousTempDir := config.TempDir
	root := t.TempDir()
	config.TempDir = root
	t.Cleanup(func() {
		config.TempDir = previousTempDir
	})

	jobID := "completed-playlist-reload-test"
	token := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	outputPath := filepath.Join(root, jobID+".zip")
	if err := os.WriteFile(outputPath, []byte("playlist zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := newState()
	state.SetAsyncJob(jobID, &AsyncJob{
		Status:          "complete",
		Progress:        100,
		Message:         "328 videos ready to download",
		CreatedAt:       time.Now(),
		Type:            "playlist",
		OutputPath:      outputPath,
		OutputFilename:  "playlist.zip",
		MimeType:        "application/zip",
		DownloadToken:   token,
		FileName:        "playlist.zip",
		FileSize:        12,
		PlaylistTitle:   "playlist",
		TotalVideos:     347,
		VideosCompleted: 328,
		FailedCount:     19,
	})
	if err := state.PersistCompletedAsyncJob(jobID); err != nil {
		t.Fatal(err)
	}

	reloaded := newState()
	download := reloaded.RestoreCompletedAsyncJobByToken(token)
	if download == nil {
		t.Fatal("playlist download token was not restored")
	}
	if download.FilePath != outputPath || download.FileName != "playlist.zip" || download.FileSize != 12 {
		t.Fatalf("restored download = %#v", download)
	}
	job := reloaded.GetAsyncJob(jobID)
	if job == nil {
		t.Fatal("playlist job was not restored")
	}
	status := job.GetPlaylistStatus()
	if status["downloadToken"] != token || status["videosCompleted"] != 328 || status["failedCount"] != 19 {
		t.Fatalf("restored playlist status = %#v", status)
	}
}
