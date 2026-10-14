package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
)

func TestPackagePlaylistFilesStagesOutsidePlaylistVolume(t *testing.T) {
	previousPlaylistDir := config.TempDirs["playlist"]
	previousStageDir := playlistZipStageDir
	previousWriter := playlistZipWriter
	previousRename := playlistRenameFile
	root := t.TempDir()
	playlistRoot := filepath.Join(root, "playlist-volume")
	stageRoot := filepath.Join(root, "zip-stage")
	jobID := "playlist-zip-stage-test"
	playlistDir := filepath.Join(playlistRoot, jobID)
	inputPath := filepath.Join(playlistDir, "track.mp3")
	if err := os.MkdirAll(playlistDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}

	config.TempDirs["playlist"] = playlistRoot
	playlistZipStageDir = func() string { return stageRoot }
	var writerPath string
	playlistZipWriter = func(path string, files []string) error {
		writerPath = path
		if len(files) != 1 || files[0] != inputPath {
			t.Fatalf("files = %v", files)
		}
		if _, err := os.Stat(inputPath); err != nil {
			t.Fatalf("input removed before ZIP completed: %v", err)
		}
		return os.WriteFile(path, []byte("zip"), 0o600)
	}
	playlistRenameFile = func(string, string) error { return errors.New("cross-device") }
	t.Cleanup(func() {
		config.TempDirs["playlist"] = previousPlaylistDir
		playlistZipStageDir = previousStageDir
		playlistZipWriter = previousWriter
		playlistRenameFile = previousRename
	})

	zipPath, err := packagePlaylistFiles(jobID, playlistDir, []string{inputPath})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(writerPath) != stageRoot {
		t.Fatalf("ZIP writer path = %s, want stage directory %s", writerPath, stageRoot)
	}
	expectedPath := filepath.Join(playlistRoot, jobID+".zip")
	if zipPath != expectedPath {
		t.Fatalf("ZIP path = %s, want %s", zipPath, expectedPath)
	}
	if data, err := os.ReadFile(zipPath); err != nil || string(data) != "zip" {
		t.Fatalf("final ZIP = %q, %v", data, err)
	}
	if _, err := os.Stat(playlistDir); !os.IsNotExist(err) {
		t.Fatalf("playlist directory still exists: %v", err)
	}
}

func TestPlaylistStopsBeforeUsingDiskReserve(t *testing.T) {
	previousDiskSpace := playlistDiskSpaceAvailableGB
	t.Cleanup(func() {
		playlistDiskSpaceAvailableGB = previousDiskSpace
	})

	for available, want := range map[float64]bool{
		4.9:                                 false,
		float64(config.DiskSpaceMinGB):      true,
		float64(config.DiskSpaceMinGB) + 10: true,
	} {
		playlistDiskSpaceAvailableGB = func() float64 { return available }
		if got := playlistHasDiskHeadroom(); got != want {
			t.Fatalf("playlistHasDiskHeadroom() with %.1fGB free = %t, want %t", available, got, want)
		}
	}
}

func TestActivePlaylistStatusSurvivesRestart(t *testing.T) {
	previousTempDir := config.TempDir
	previousPlaylistDir := config.TempDirs["playlist"]
	root := t.TempDir()
	playlistRoot := filepath.Join(root, "playlists")
	config.TempDir = root
	config.TempDirs["playlist"] = playlistRoot
	t.Cleanup(func() {
		config.TempDir = previousTempDir
		config.TempDirs["playlist"] = previousPlaylistDir
	})

	jobID := "playlist-restart-test"
	jobDir := filepath.Join(playlistRoot, jobID)
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "001 - first.mp4"), []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := activePlaylistCheckpoint{
		ID:            jobID,
		URL:           "https://www.youtube.com/playlist?list=restart-test",
		OutputExt:     "mp4",
		Quality:       "1080p",
		Container:     "mp4",
		AudioFormat:   "mp3",
		AudioBitrate:  "320",
		NextVideo:     1,
		CreatedAt:     time.Now(),
		PlaylistTitle: "Restart Test",
		TotalVideos:   3,
		CurrentVideo:  1,
	}
	if err := persistActivePlaylistCheckpoint(record); err != nil {
		t.Fatal(err)
	}
	services.Global.DeleteAsyncJob(jobID)
	t.Cleanup(func() { services.Global.DeleteAsyncJob(jobID) })

	records, err := loadActivePlaylistCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("loaded checkpoints = %d, want 1", len(records))
	}
	job, nextVideo, files := restoreActivePlaylistJob(records[0])
	if job == nil {
		t.Fatal("playlist job was not restored")
	}
	if nextVideo != 2 {
		t.Fatalf("next video = %d, want 2", nextVideo)
	}
	if len(files) != 1 {
		t.Fatalf("restored files = %d, want 1", len(files))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/playlist/status/"+jobID, nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("jobId", jobID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext))
	res := httptest.NewRecorder()
	handlePlaylistStatus(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", res.Code, res.Body.String())
	}
	var status map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["status"] != "recovering" {
		t.Fatalf("status = %v, want recovering", status["status"])
	}
	if status["videosCompleted"] != float64(1) {
		t.Fatalf("videos completed = %v, want 1", status["videosCompleted"])
	}
}

func TestRecoveredPlaylistErrorPreservesMediaAndCheckpoint(t *testing.T) {
	previousTempDir := config.TempDir
	previousPlaylistDir := config.TempDirs["playlist"]
	root := t.TempDir()
	playlistRoot := filepath.Join(root, "playlists")
	config.TempDir = root
	config.TempDirs["playlist"] = playlistRoot
	t.Cleanup(func() {
		config.TempDir = previousTempDir
		config.TempDirs["playlist"] = previousPlaylistDir
	})

	jobID := "playlist-recovery-error-test"
	jobDir := filepath.Join(playlistRoot, jobID)
	mediaPath := filepath.Join(jobDir, "001 - recovered.mp4")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mediaPath, []byte("media"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := persistActivePlaylistCheckpoint(activePlaylistCheckpoint{
		ID:        jobID,
		URL:       "https://www.youtube.com/playlist?list=recovery-error-test",
		OutputExt: "mp4",
		NextVideo: 2,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	job := &services.AsyncJob{Status: "recovering", Type: "playlist", CreatedAt: time.Now()}
	playlistError(jobID, job, &services.ProcessInfo{}, jobDir, errors.New("temporary source failure"), true)
	if _, err := os.Stat(mediaPath); err != nil {
		t.Fatalf("recovered media was removed: %v", err)
	}
	if _, err := os.Stat(activePlaylistCheckpointPath(jobID)); err != nil {
		t.Fatalf("recovery checkpoint was removed: %v", err)
	}
}
