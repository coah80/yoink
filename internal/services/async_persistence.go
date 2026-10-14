package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/coah80/yoink/internal/config"
)

type completedAsyncJob struct {
	ID              string        `json:"id"`
	CreatedAt       time.Time     `json:"createdAt"`
	Type            string        `json:"type,omitempty"`
	URL             string        `json:"url,omitempty"`
	Format          string        `json:"format,omitempty"`
	Message         string        `json:"message,omitempty"`
	OutputPath      string        `json:"outputPath"`
	OutputFilename  string        `json:"outputFilename"`
	MimeType        string        `json:"mimeType"`
	DownloadToken   string        `json:"downloadToken,omitempty"`
	FileName        string        `json:"fileName,omitempty"`
	FileSize        int64         `json:"fileSize,omitempty"`
	PlaylistTitle   string        `json:"playlistTitle,omitempty"`
	TotalVideos     int           `json:"totalVideos,omitempty"`
	VideosCompleted int           `json:"videosCompleted,omitempty"`
	CurrentVideo    int           `json:"currentVideo,omitempty"`
	FailedVideos    []FailedVideo `json:"failedVideos,omitempty"`
	FailedCount     int           `json:"failedCount,omitempty"`
}

var recoverableAsyncJobID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func completedAsyncJobsDir() string {
	return filepath.Join(config.TempDir, "state", "async-jobs")
}

func completedAsyncJobPath(jobID string) string {
	sum := sha256.Sum256([]byte(jobID))
	return filepath.Join(completedAsyncJobsDir(), hex.EncodeToString(sum[:])+".json")
}

func validCompletedAsyncOutput(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return false
	}
	root, err := filepath.Abs(config.TempDir)
	if err != nil {
		return false
	}
	resolved, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(root, resolved)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func persistCompletedAsyncJob(job completedAsyncJob) error {
	if job.ID == "" || !validCompletedAsyncOutput(job.OutputPath) {
		return fmt.Errorf("completed async job is invalid")
	}
	dir := completedAsyncJobsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".job-*.tmp")
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
	if err := os.Rename(tempPath, completedAsyncJobPath(job.ID)); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func (s *State) PersistCompletedAsyncJob(jobID string) error {
	job := s.GetAsyncJob(jobID)
	if job == nil {
		return fmt.Errorf("async job not found")
	}
	job.mu.RLock()
	if job.Status != "complete" {
		job.mu.RUnlock()
		return fmt.Errorf("async job is not complete")
	}
	record := completedAsyncJob{
		ID:              jobID,
		CreatedAt:       job.CreatedAt,
		Type:            job.Type,
		URL:             job.URL,
		Format:          job.Format,
		Message:         job.Message,
		OutputPath:      job.OutputPath,
		OutputFilename:  job.OutputFilename,
		MimeType:        job.MimeType,
		DownloadToken:   job.DownloadToken,
		FileName:        job.FileName,
		FileSize:        job.FileSize,
		PlaylistTitle:   job.PlaylistTitle,
		TotalVideos:     job.TotalVideos,
		VideosCompleted: job.VideosCompleted,
		CurrentVideo:    job.CurrentVideo,
		FailedVideos:    append([]FailedVideo(nil), job.FailedVideos...),
		FailedCount:     job.FailedCount,
	}
	job.mu.RUnlock()
	return persistCompletedAsyncJob(record)
}

func (s *State) restoreCompletedAsyncJob(record completedAsyncJob) *AsyncJob {
	if record.ID == "" || !validCompletedAsyncOutput(record.OutputPath) {
		return nil
	}
	if record.OutputFilename == "" {
		record.OutputFilename = record.ID + filepath.Ext(record.OutputPath)
	}
	if record.MimeType == "" {
		record.MimeType = mimeTypeForRecoveredAsyncOutput(record.OutputPath)
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	message := record.Message
	if message == "" {
		message = "Conversion complete!"
	}
	fileSize := record.FileSize
	if fileSize == 0 {
		if info, err := os.Stat(record.OutputPath); err == nil {
			fileSize = info.Size()
		}
	}
	job := &AsyncJob{
		Status:          "complete",
		Progress:        100,
		Message:         message,
		CreatedAt:       record.CreatedAt,
		Type:            record.Type,
		URL:             record.URL,
		Format:          record.Format,
		OutputPath:      record.OutputPath,
		OutputFilename:  record.OutputFilename,
		MimeType:        record.MimeType,
		DownloadToken:   record.DownloadToken,
		FileName:        record.FileName,
		FileSize:        fileSize,
		PlaylistTitle:   record.PlaylistTitle,
		TotalVideos:     record.TotalVideos,
		VideosCompleted: record.VideosCompleted,
		CurrentVideo:    record.CurrentVideo,
		FailedVideos:    append([]FailedVideo(nil), record.FailedVideos...),
		FailedCount:     record.FailedCount,
	}
	s.SetAsyncJob(record.ID, job)
	if record.Type == "playlist" && record.DownloadToken != "" {
		fileName := record.FileName
		if fileName == "" {
			fileName = record.OutputFilename
		}
		s.SetBotDownload(record.DownloadToken, &BotDownload{
			JobID:         record.ID,
			FilePath:      record.OutputPath,
			FileName:      fileName,
			FileSize:      fileSize,
			MimeType:      record.MimeType,
			CreatedAt:     record.CreatedAt,
			IsWebPlaylist: true,
		})
	}
	return job
}

func (s *State) RestoreCompletedAsyncJobByToken(token string) *BotDownload {
	if download := s.GetBotDownload(token); download != nil {
		return download
	}
	if !recoverableAsyncJobID.MatchString(token) {
		return nil
	}
	entries, err := os.ReadDir(completedAsyncJobsDir())
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(completedAsyncJobsDir(), entry.Name()))
		if err != nil {
			continue
		}
		var record completedAsyncJob
		if json.Unmarshal(data, &record) != nil || record.DownloadToken != token {
			continue
		}
		if s.restoreCompletedAsyncJob(record) == nil {
			return nil
		}
		return s.GetBotDownload(token)
	}
	return nil
}

func (s *State) LoadCompletedAsyncJobs() (int, error) {
	dir := completedAsyncJobsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record completedAsyncJob
		if json.Unmarshal(data, &record) != nil || s.restoreCompletedAsyncJob(record) == nil {
			continue
		}
		loaded++
	}
	return loaded, nil
}

func (s *State) RestoreCompletedAsyncJob(jobID string) *AsyncJob {
	if job := s.GetAsyncJob(jobID); job != nil {
		return job
	}
	if !recoverableAsyncJobID.MatchString(jobID) {
		return nil
	}
	if data, err := os.ReadFile(completedAsyncJobPath(jobID)); err == nil {
		var record completedAsyncJob
		if json.Unmarshal(data, &record) == nil && record.ID == jobID {
			if job := s.restoreCompletedAsyncJob(record); job != nil {
				return job
			}
		}
	}
	patterns := []string{
		filepath.Join(config.TempDirs["convert"], jobID+"-converted.*"),
		filepath.Join(config.TempDirs["compress"], jobID+"-compressed.*"),
	}
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			info, err := os.Stat(path)
			if err != nil || info.Size() == 0 {
				continue
			}
			record := completedAsyncJob{
				ID:             jobID,
				CreatedAt:      info.ModTime(),
				OutputPath:     path,
				OutputFilename: jobID + filepath.Ext(path),
				MimeType:       mimeTypeForRecoveredAsyncOutput(path),
			}
			job := s.restoreCompletedAsyncJob(record)
			if job != nil {
				_ = persistCompletedAsyncJob(record)
				return job
			}
		}
	}
	return nil
}

func (s *State) CompletedAsyncOutputPaths() []string {
	s.muAsync.RLock()
	paths := make([]string, 0)
	for _, job := range s.asyncJobs {
		path, _, _, status := job.GetDownloadInfo()
		if status == "complete" && path != "" {
			paths = append(paths, path)
		}
	}
	s.muAsync.RUnlock()
	return paths
}

func (s *State) RemoveCompletedAsyncJob(jobID string) {
	_ = os.Remove(completedAsyncJobPath(jobID))
}

func mimeTypeForRecoveredAsyncOutput(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a":
		return "audio/mp4"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	case ".zip":
		return "application/zip"
	case ".webm":
		return "video/webm"
	case ".mkv":
		return "video/x-matroska"
	case ".mov":
		return "video/quicktime"
	default:
		return "video/mp4"
	}
}
