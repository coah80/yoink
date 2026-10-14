package routes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
)

type playlistRunOptions struct {
	services.DownloadPreferences
	URL          string
	IsAudio      bool
	AudioFormat  string
	OutputExt    string
	Quality      string
	Container    string
	AudioBitrate string
	Recovering   bool
}

type activePlaylistCheckpoint struct {
	services.DownloadPreferences
	ID                string                 `json:"id"`
	URL               string                 `json:"url"`
	IsAudio           bool                   `json:"isAudio"`
	AudioFormat       string                 `json:"audioFormat"`
	OutputExt         string                 `json:"outputExt"`
	Quality           string                 `json:"quality"`
	Container         string                 `json:"container"`
	AudioBitrate      string                 `json:"audioBitrate"`
	NextVideo         int                    `json:"nextVideo"`
	CreatedAt         time.Time              `json:"createdAt"`
	PlaylistTitle     string                 `json:"playlistTitle,omitempty"`
	TotalVideos       int                    `json:"totalVideos,omitempty"`
	VideosCompleted   int                    `json:"videosCompleted,omitempty"`
	CurrentVideo      int                    `json:"currentVideo,omitempty"`
	CurrentVideoTitle string                 `json:"currentVideoTitle,omitempty"`
	Progress          float64                `json:"progress,omitempty"`
	FailedVideos      []services.FailedVideo `json:"failedVideos,omitempty"`
	FailedCount       int                    `json:"failedCount,omitempty"`
}

var activePlaylistID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func activePlaylistCheckpointsDir() string {
	return filepath.Join(config.TempDir, "state", "active-playlists")
}

func activePlaylistCheckpointPath(jobID string) string {
	sum := sha256.Sum256([]byte(jobID))
	return filepath.Join(activePlaylistCheckpointsDir(), hex.EncodeToString(sum[:])+".json")
}

func persistActivePlaylistCheckpoint(record activePlaylistCheckpoint) error {
	if !activePlaylistID.MatchString(record.ID) || record.URL == "" || record.OutputExt == "" {
		return fmt.Errorf("active playlist checkpoint is invalid")
	}
	if record.NextVideo < 1 {
		record.NextVideo = 1
	}
	dir := activePlaylistCheckpointsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".playlist-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	if err := temp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, activePlaylistCheckpointPath(record.ID)); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func removeActivePlaylistCheckpoint(jobID string) {
	_ = os.Remove(activePlaylistCheckpointPath(jobID))
}

func loadActivePlaylistCheckpoints() ([]activePlaylistCheckpoint, error) {
	dir := activePlaylistCheckpointsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	records := make([]activePlaylistCheckpoint, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var record activePlaylistCheckpoint
		if json.Unmarshal(data, &record) != nil || !activePlaylistID.MatchString(record.ID) || record.URL == "" || record.OutputExt == "" {
			continue
		}
		jobDir := filepath.Join(config.TempDirs["playlist"], record.ID)
		if info, err := os.Stat(jobDir); err != nil || !info.IsDir() {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].CreatedAt.Before(records[j].CreatedAt) })
	return records, nil
}

func checkpointActivePlaylist(jobID string, job *services.AsyncJob, options playlistRunOptions, nextVideo int) error {
	job.Lock()
	record := activePlaylistCheckpoint{
		ID:                  jobID,
		URL:                 options.URL,
		IsAudio:             options.IsAudio,
		AudioFormat:         options.AudioFormat,
		OutputExt:           options.OutputExt,
		Quality:             options.Quality,
		Container:           options.Container,
		AudioBitrate:        options.AudioBitrate,
		DownloadPreferences: options.DownloadPreferences,
		NextVideo:           nextVideo,
		CreatedAt:           job.CreatedAt,
		PlaylistTitle:       job.PlaylistTitle,
		TotalVideos:         job.TotalVideos,
		VideosCompleted:     job.VideosCompleted,
		CurrentVideo:        job.CurrentVideo,
		CurrentVideoTitle:   job.CurrentVideoTitle,
		Progress:            job.Progress,
		FailedVideos:        append([]services.FailedVideo(nil), job.FailedVideos...),
		FailedCount:         job.FailedCount,
	}
	job.Unlock()
	return persistActivePlaylistCheckpoint(record)
}

func playlistExistingFiles(playlistDir, outputExt string, beforeVideo int) ([]string, int) {
	entries, err := os.ReadDir(playlistDir)
	if err != nil {
		return nil, 0
	}
	type numberedFile struct {
		number int
		path   string
	}
	files := make([]numberedFile, 0)
	maxVideo := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), "."+outputExt) {
			continue
		}
		prefix, _, found := strings.Cut(entry.Name(), " - ")
		if !found {
			continue
		}
		number, err := strconv.Atoi(prefix)
		if err != nil || number < 1 || beforeVideo > 0 && number >= beforeVideo {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}
		files = append(files, numberedFile{number: number, path: filepath.Join(playlistDir, entry.Name())})
		if number > maxVideo {
			maxVideo = number
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].number < files[j].number })
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.path
	}
	return paths, maxVideo
}

func restoreActivePlaylistJob(record activePlaylistCheckpoint) (*services.AsyncJob, int, []string) {
	playlistDir := filepath.Join(config.TempDirs["playlist"], record.ID)
	files, maxVideo := playlistExistingFiles(playlistDir, record.OutputExt, 0)
	nextVideo := record.NextVideo
	if nextVideo < 1 {
		nextVideo = 1
	}
	if maxVideo >= nextVideo {
		nextVideo = maxVideo + 1
	}
	createdAt := record.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	currentVideo := record.CurrentVideo
	if maxVideo > currentVideo {
		currentVideo = maxVideo
	}
	job := &services.AsyncJob{
		Status:            "recovering",
		Progress:          record.Progress,
		Message:           "recovering playlist after server restart...",
		CreatedAt:         createdAt,
		Type:              "playlist",
		URL:               record.URL,
		Format:            record.OutputExt,
		PlaylistTitle:     record.PlaylistTitle,
		TotalVideos:       record.TotalVideos,
		VideosCompleted:   len(files),
		CurrentVideo:      currentVideo,
		CurrentVideoTitle: record.CurrentVideoTitle,
		FailedVideos:      append([]services.FailedVideo(nil), record.FailedVideos...),
		FailedCount:       record.FailedCount,
	}
	services.Global.SetAsyncJob(record.ID, job)
	return job, nextVideo, files
}

func ResumeActivePlaylists() {
	records, err := loadActivePlaylistCheckpoints()
	if err != nil {
		log.Printf("[Recovery] Failed to load active playlists: %v", err)
		return
	}
	for _, record := range records {
		if services.Global.GetAsyncJob(record.ID) != nil {
			removeActivePlaylistCheckpoint(record.ID)
			continue
		}
		job, nextVideo, _ := restoreActivePlaylistJob(record)
		options := playlistRunOptions{
			URL:                 record.URL,
			IsAudio:             record.IsAudio,
			AudioFormat:         record.AudioFormat,
			OutputExt:           record.OutputExt,
			Quality:             record.Quality,
			Container:           record.Container,
			AudioBitrate:        record.AudioBitrate,
			DownloadPreferences: record.DownloadPreferences,
			Recovering:          true,
		}
		go func(jobID string, job *services.AsyncJob, options playlistRunOptions, resumeFrom int) {
			check := services.Global.WaitForJobSlot(context.Background(), "playlist")
			if !check.OK {
				job.Lock()
				job.Message = "playlist recovery paused: " + check.Reason
				job.Unlock()
				log.Printf("[Recovery] Playlist %s paused: %s", jobID, check.Reason)
				return
			}
			log.Printf("[Recovery] Resuming playlist %s from video %d", jobID, resumeFrom)
			processPlaylistAsync(jobID, job, options, resumeFrom)
		}(record.ID, job, options, nextVideo)
	}
	if len(records) > 0 {
		log.Printf("[Recovery] Restored %d active playlist(s)", len(records))
	}
}
