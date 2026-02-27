package routes

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/coah80/yoink/internal/alerts"
	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
	"github.com/coah80/yoink/internal/util"
)

func PlaylistRoutes(r chi.Router) {
	r.Post("/api/playlist/start", handlePlaylistStart)
	r.Get("/api/playlist/status/{jobId}", handlePlaylistStatus)
	r.Get("/api/playlist/download/{token}", handlePlaylistDownload)
}

func handlePlaylistStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL          string `json:"url"`
		Format       string `json:"format"`
		Quality      string `json:"quality"`
		Container    string `json:"container"`
		AudioFormat  string `json:"audioFormat"`
		AudioBitrate string `json:"audioBitrate"`
		ClientID     string `json:"clientId"`
		ResumeFrom   int    `json:"resumeFrom"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, 400, map[string]string{"error": "Invalid request body"})
		return
	}

	if body.Format == "" {
		body.Format = "video"
	}
	if body.Quality == "" {
		body.Quality = "1080p"
	}
	if body.Container == "" {
		body.Container = "mp4"
	}
	if body.AudioFormat == "" {
		body.AudioFormat = "mp3"
	}
	if body.AudioBitrate == "" {
		body.AudioBitrate = "320"
	}
	if body.ResumeFrom < 1 {
		body.ResumeFrom = 1
	}

	check := util.ValidateURL(body.URL)
	if !check.Valid {
		respondJSON(w, 400, map[string]string{"error": check.Error})
		return
	}

	if body.ClientID != "" {
		if services.Global.GetClientJobCount(body.ClientID) >= config.MaxJobsPerClient {
			respondJSON(w, 429, map[string]string{"error": fmt.Sprintf("Too many active jobs. Maximum %d concurrent jobs per user.", config.MaxJobsPerClient)})
			return
		}
	}

	jobCheck := services.Global.CanStartJob("playlist")
	if !jobCheck.OK {
		respondJSON(w, 503, map[string]string{"error": jobCheck.Reason})
		return
	}

	jobID := uuid.New().String()
	isAudio := body.Format == "audio"
	outputExt := body.Container
	if isAudio {
		outputExt = body.AudioFormat
	}

	if body.ClientID != "" {
		services.Global.RegisterClient(body.ClientID)
		services.Global.LinkJobToClient(jobID, body.ClientID)
	}

	job := &services.AsyncJob{
		Status:    "starting",
		Message:   "getting playlist info...",
		CreatedAt: time.Now(),
		URL:       body.URL,
		Format:    outputExt,
		Type:      "playlist",
	}
	services.Global.SetAsyncJob(jobID, job)

	respondJSON(w, 200, map[string]string{"jobId": jobID})

	go processPlaylistAsync(jobID, job, body.URL, isAudio, body.AudioFormat, outputExt, body.Quality, body.Container, body.AudioBitrate, body.ResumeFrom)
}

func processPlaylistAsync(jobID string, job *services.AsyncJob, rawURL string, isAudio bool, audioFormat, outputExt, quality, container, audioBitrate string, resumeFrom int) {
	playlistDir := filepath.Join(config.TempDirs["playlist"], jobID)
	os.MkdirAll(playlistDir, 0755)

	ctx, cancel := context.WithCancel(context.Background())
	processInfo := &services.ProcessInfo{
		TempDir:    playlistDir,
		JobType:    "playlist",
		CancelFunc: cancel,
	}
	services.Global.SetProcess(jobID, processInfo)

	jobs := services.Global.GetJobsByType()
	jobsJSON, _ := json.Marshal(jobs)
	log.Printf("[Queue] Async playlist started. Active: %s", jobsJSON)

	defer cancel()

	playlistInfo, err := services.GetPlaylistInfo(ctx, rawURL, true)
	if err != nil {
		playlistError(jobID, job, processInfo, playlistDir, err)
		return
	}

	if playlistInfo.Count > config.MaxPlaylistVideos {
		playlistError(jobID, job, processInfo, playlistDir, fmt.Errorf("Playlist too large. Maximum %d videos allowed. This playlist has %d videos.", config.MaxPlaylistVideos, playlistInfo.Count))
		return
	}

	totalVideos := playlistInfo.Count
	playlistTitle := playlistInfo.Title
	startIdx := resumeFrom - 1
	if startIdx >= len(playlistInfo.Entries) {
		startIdx = len(playlistInfo.Entries) - 1
	}
	if startIdx < 0 {
		startIdx = 0
	}
	isResuming := startIdx > 0

	var msg string
	if isResuming {
		msg = fmt.Sprintf("resuming from video %d/%d", resumeFrom, totalVideos)
	} else {
		msg = fmt.Sprintf("found %d videos", totalVideos)
	}

	job.Lock()
	job.Status = "downloading"
	job.PlaylistTitle = playlistTitle
	job.TotalVideos = totalVideos
	job.Message = msg
	job.Unlock()

	formatStr := fmt.Sprintf("%s %s", quality, container)
	if isAudio {
		formatStr = audioFormat
	}
	p0 := float64(0)
	services.Global.SendProgress(jobID, "playlist-info", msg, &p0, map[string]interface{}{
		"playlistTitle": playlistTitle, "totalVideos": totalVideos,
		"currentVideo": startIdx, "currentVideoTitle": "", "format": formatStr,
	})

	var downloadedFiles []string
	var failedVideos []services.FailedVideo

	for i := startIdx; i < len(playlistInfo.Entries); i++ {
		if processInfo.IsCancelled() {
			playlistError(jobID, job, processInfo, playlistDir, fmt.Errorf("Download cancelled"))
			return
		}
		if processInfo.IsFinishEarly() {
			log.Printf("[%s] Finishing early after %d videos", jobID, len(downloadedFiles))
			break
		}

		entry := playlistInfo.Entries[i]
		videoNum := i + 1
		videoTitle := orDefault(entry.Title, fmt.Sprintf("Video %d", videoNum))
		videoURL := entry.URL
		if videoURL == "" && entry.ID != "" {
			videoURL = "https://www.youtube.com/watch?v=" + entry.ID
		}
		if videoURL == "" {
			continue
		}

		safeTitle := util.SanitizeFilename(videoTitle)
		if len(safeTitle) > 100 {
			safeTitle = safeTitle[:100]
		}
		videoFile := filepath.Join(playlistDir, fmt.Sprintf("%03d - %s.%s", videoNum, safeTitle, outputExt))

		progress := float64(videoNum-1) / float64(totalVideos) * 100
		job.Lock()
		job.CurrentVideo = videoNum
		job.CurrentVideoTitle = videoTitle
		job.Progress = progress
		job.Message = fmt.Sprintf("downloading %d/%d: %s", videoNum, totalVideos, videoTitle)
		job.Unlock()
		services.Global.SendProgress(jobID, "downloading", fmt.Sprintf("Downloading %d/%d: %s", videoNum, totalVideos, videoTitle),
			&progress, map[string]interface{}{
				"playlistTitle": playlistTitle, "totalVideos": totalVideos,
				"currentVideo": videoNum, "currentVideoTitle": videoTitle,
				"format": formatStr, "failedVideos": failedVideos, "failedCount": len(failedVideos),
			})

		actualURL := videoURL
		isYT := strings.Contains(actualURL, "youtube.com") || strings.Contains(actualURL, "youtu.be")
		var tempPath string

		downloadErr := func() error {
			if isYT {
				result, err := services.DownloadViaYtdlp(ctx, actualURL, fmt.Sprintf("temp_%d", videoNum), services.DownloadOpts{
					IsAudio: isAudio, Quality: quality, Container: container,
					TempDir: playlistDir, ProcessInfo: processInfo, UseProxy: true,
					OnProgress: func(prog float64, speed, eta string) {
						overallProg := (float64(videoNum-1)/float64(totalVideos))*100 + (prog / float64(totalVideos))
						job.Lock()
						job.Progress = overallProg
						job.Speed = speed
						job.ETA = eta
						job.Unlock()
					},
				})
				if err != nil {
					cobaltResult, cobaltErr := services.DownloadViaCobalt(ctx, actualURL, fmt.Sprintf("%s-v%d", jobID, videoNum), isAudio, nil,
						services.CobaltDownloadOpts{OutputDir: playlistDir, MaxRetries: 3, RetryDelay: 2 * time.Second})
					if cobaltErr != nil {
						return cobaltErr
					}
					tempPath = cobaltResult.FilePath
				} else {
					tempPath = result.Path
				}
			} else {
				result, err := services.DownloadViaYtdlp(ctx, actualURL, fmt.Sprintf("temp_%d", videoNum), services.DownloadOpts{
					IsAudio: isAudio, Quality: quality, Container: container,
					TempDir: playlistDir, ProcessInfo: processInfo,
				})
				if err != nil {
					return err
				}
				tempPath = result.Path
			}
			return nil
		}()

		if downloadErr != nil {
			errMsg := downloadErr.Error()
			if errMsg == "Cancelled" || errMsg == "Download cancelled" {
				playlistError(jobID, job, processInfo, playlistDir, downloadErr)
				return
			}
			failedVideos = append(failedVideos, services.FailedVideo{Num: videoNum, Title: videoTitle, Reason: util.ToUserError(errMsg)})
			job.Lock()
			job.FailedVideos = failedVideos
			job.FailedCount = len(failedVideos)
			job.Unlock()
			continue
		}

		if tempPath != "" {
			if _, err := os.Stat(tempPath); err == nil {
				processed, err := services.ProcessVideo(tempPath, videoFile, services.ProcessVideoOpts{
					IsAudio: isAudio, AudioFormat: audioFormat, AudioBitrate: audioBitrate, Container: container, JobID: jobID,
				})
				if err == nil {
					if processed.Skipped && tempPath != videoFile {
						os.Rename(tempPath, videoFile)
					}
					downloadedFiles = append(downloadedFiles, videoFile)
					job.Lock()
					job.VideosCompleted = len(downloadedFiles)
					job.Unlock()
					log.Printf("[%s] Video %d complete", jobID, videoNum)
				}
			}
		}
	}

	if len(downloadedFiles) == 0 {
		playlistError(jobID, job, processInfo, playlistDir, fmt.Errorf("No videos were successfully downloaded"))
		return
	}

	job.Lock()
	job.Status = "zipping"
	job.Progress = 95
	job.Message = fmt.Sprintf("creating zip with %d videos...", len(downloadedFiles))
	job.Unlock()
	p95 := float64(95)
	services.Global.SendProgress(jobID, "zipping", fmt.Sprintf("Creating zip file with %d videos...", len(downloadedFiles)),
		&p95, map[string]interface{}{"playlistTitle": playlistTitle, "totalVideos": totalVideos, "downloadedCount": len(downloadedFiles)})

	zipPath := filepath.Join(config.TempDirs["playlist"], fmt.Sprintf("%s.zip", jobID))
	safePlaylistName := util.SanitizeFilename(orDefault(playlistTitle, "playlist"))

	if err := createZip(zipPath, downloadedFiles); err != nil {
		playlistError(jobID, job, processInfo, playlistDir, fmt.Errorf("Failed to create zip: %v", err))
		return
	}

	os.RemoveAll(playlistDir)

	stat, err := os.Stat(zipPath)
	if err != nil {
		playlistError(jobID, job, processInfo, "", fmt.Errorf("zip file not found after creation"))
		return
	}
	token := randomToken()
	fileName := safePlaylistName + ".zip"

	services.Global.SetBotDownload(token, &services.BotDownload{
		FilePath:      zipPath,
		FileName:      fileName,
		FileSize:      stat.Size(),
		MimeType:      "application/zip",
		CreatedAt:     time.Now(),
		IsWebPlaylist: true,
	})

	job.Lock()
	job.Status = "complete"
	job.Progress = 100
	job.Message = fmt.Sprintf("%d videos ready to download", len(downloadedFiles))
	job.DownloadToken = token
	job.FileName = fileName
	job.FileSize = stat.Size()
	job.FailedVideos = failedVideos
	job.FailedCount = len(failedVideos)
	job.Unlock()

	p100 := float64(100)
	services.Global.SendProgress(jobID, "complete", fmt.Sprintf("%d videos ready!", len(downloadedFiles)),
		&p100, map[string]interface{}{
			"playlistTitle": playlistTitle, "totalVideos": totalVideos,
			"downloadedCount": len(downloadedFiles), "failedVideos": failedVideos,
			"failedCount": len(failedVideos), "downloadToken": token,
		})

	services.Global.ReleaseJob(jobID)
	log.Println("[Queue] Async playlist complete.")
}

func playlistError(jobID string, job *services.AsyncJob, processInfo *services.ProcessInfo, playlistDir string, err error) {
	log.Printf("[%s] Async playlist error: %s", jobID, err)
	alerts.PlaylistFailed(jobID, "", err)
	job.Lock()
	job.Status = "error"
	job.Message = util.ToUserError(err.Error())
	job.Unlock()
	if !processInfo.IsCancelled() {
		services.Global.SendProgressSimple(jobID, "error", util.ToUserError(err.Error()))
	}
	services.Global.ReleaseJob(jobID)
	log.Println("[Queue] Async playlist error.")
	os.RemoveAll(playlistDir)
}

func handlePlaylistStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	job := services.Global.GetAsyncJob(jobID)
	if job == nil {
		respondJSON(w, 404, map[string]string{"error": "Job not found"})
		return
	}
	respondJSON(w, 200, job.GetPlaylistStatus())
}

func handlePlaylistDownload(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	data := services.Global.GetBotDownload(token)
	if data == nil {
		respondJSON(w, 404, map[string]string{"error": "Download not found or expired"})
		return
	}
	stat, err := os.Stat(data.FilePath)
	if err != nil {
		services.Global.DeleteBotDownload(token)
		respondJSON(w, 404, map[string]string{"error": "File no longer available"})
		return
	}
	asciiFilename := toASCIIFilename(data.FileName)

	w.Header().Set("Content-Type", data.MimeType)
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, asciiFilename, url.PathEscape(data.FileName)))

	f, err := os.Open(data.FilePath)
	if err != nil {
		respondJSON(w, 500, map[string]string{"error": "Failed to open file"})
		return
	}
	defer f.Close()
	io.Copy(w, f)

	data.Downloaded = true
	go func() {
		time.Sleep(30 * time.Second)
		services.Global.DeleteBotDownload(token)
		os.Remove(data.FilePath)
		short := token
		if len(short) > 8 {
			short = short[:8]
		}
		log.Printf("[Playlist] Token %s... cleaned up after download", short)
	}()
}

func createZip(zipPath string, files []string) error {
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	for _, filePath := range files {
		entry, err := zw.Create(filepath.Base(filePath))
		if err != nil {
			return err
		}
		src, err := os.Open(filePath)
		if err != nil {
			return err
		}
		io.Copy(entry, src)
		src.Close()
	}
	return nil
}

func ptrFloat(f float64) *float64 {
	return &f
}
