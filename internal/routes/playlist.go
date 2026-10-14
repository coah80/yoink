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

var playlistZipStageDir = os.TempDir
var playlistZipWriter = createZip
var playlistRenameFile = os.Rename
var playlistDiskSpaceAvailableGB = func() float64 {
	space, err := util.GetDiskSpace(config.TempDirs["playlist"])
	if err != nil {
		return 0
	}
	return space.AvailGB
}

func handlePlaylistStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		services.DownloadPreferences
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
	if err := body.DownloadPreferences.Validate(); err != nil {
		respondJSON(w, 400, map[string]string{"error": err.Error()})
		return
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

	body.URL = util.NormalizeYouTubeURL(body.URL)
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
	options := playlistRunOptions{
		DownloadPreferences: body.DownloadPreferences,
		URL:                 body.URL,
		IsAudio:             isAudio,
		AudioFormat:         body.AudioFormat,
		OutputExt:           outputExt,
		Quality:             body.Quality,
		Container:           body.Container,
		AudioBitrate:        body.AudioBitrate,
	}
	playlistDir := filepath.Join(config.TempDirs["playlist"], jobID)
	if err := os.MkdirAll(playlistDir, 0o755); err != nil {
		services.Global.DeleteAsyncJob(jobID)
		services.Global.DecrementJob("playlist")
		services.Global.UnlinkJobFromClient(jobID)
		respondJSON(w, 500, map[string]string{"error": "Failed to prepare playlist download"})
		return
	}
	if err := checkpointActivePlaylist(jobID, job, options, body.ResumeFrom); err != nil {
		log.Printf("[%s] Failed to checkpoint playlist start: %v", jobID, err)
	}

	respondJSON(w, 200, map[string]string{"jobId": jobID})

	go processPlaylistAsync(jobID, job, options, body.ResumeFrom)
}

func processPlaylistAsync(jobID string, job *services.AsyncJob, options playlistRunOptions, resumeFrom int) {
	playlistDir := filepath.Join(config.TempDirs["playlist"], jobID)
	if err := os.MkdirAll(playlistDir, 0o755); err != nil {
		log.Printf("[%s] Failed to prepare playlist directory: %v", jobID, err)
		services.Global.DecrementJob("playlist")
		return
	}

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

	playlistInfo, err := services.GetPlaylistInfo(ctx, options.URL, true)
	if err != nil {
		playlistError(jobID, job, processInfo, playlistDir, err, options.Recovering)
		return
	}

	totalVideos := playlistInfo.Count
	if totalVideos == 0 {
		totalVideos = len(playlistInfo.Entries)
	}
	if len(playlistInfo.Entries) == 0 {
		playlistError(jobID, job, processInfo, playlistDir, fmt.Errorf("No videos found in playlist"), options.Recovering)
		return
	}
	startIdx := resumeFrom - 1
	if startIdx > len(playlistInfo.Entries) {
		if !options.Recovering {
			playlistError(jobID, job, processInfo, playlistDir, fmt.Errorf("Resume point is past the end of the playlist. This playlist has %d videos.", len(playlistInfo.Entries)), options.Recovering)
			return
		}
		startIdx = len(playlistInfo.Entries)
	}
	remainingVideos := len(playlistInfo.Entries) - startIdx
	if remainingVideos > config.MaxPlaylistVideos {
		playlistError(jobID, job, processInfo, playlistDir, fmt.Errorf("Playlist chunk too large. Maximum %d videos allowed per run. Start later in the playlist or finish early.", config.MaxPlaylistVideos), options.Recovering)
		return
	}

	playlistTitle := playlistInfo.Title
	isResuming := startIdx > 0
	startVideo := startIdx + 1

	var msg string
	if isResuming {
		msg = fmt.Sprintf("resuming from video %d/%d", startVideo, totalVideos)
	} else {
		msg = fmt.Sprintf("found %d videos", totalVideos)
	}

	job.Lock()
	job.Status = "downloading"
	job.PlaylistTitle = playlistTitle
	job.TotalVideos = totalVideos
	job.StartVideo = startVideo
	job.Message = msg
	job.Unlock()

	formatStr := fmt.Sprintf("%s %s", options.Quality, options.Container)
	if options.IsAudio {
		formatStr = options.AudioFormat
	}
	p0 := float64(0)
	services.Global.SendProgress(jobID, "playlist-info", msg, &p0, map[string]interface{}{
		"playlistTitle": playlistTitle, "totalVideos": totalVideos,
		"currentVideo": startIdx, "currentVideoTitle": "", "format": formatStr,
	})

	downloadedFiles, _ := playlistExistingFiles(playlistDir, options.OutputExt, resumeFrom)
	job.Lock()
	failedVideos := append([]services.FailedVideo(nil), job.FailedVideos...)
	job.VideosCompleted = len(downloadedFiles)
	job.Unlock()
	if err := checkpointActivePlaylist(jobID, job, options, resumeFrom); err != nil {
		log.Printf("[%s] Failed to checkpoint playlist info: %v", jobID, err)
	}

	for i := startIdx; i < len(playlistInfo.Entries); i++ {
		if processInfo.IsCancelled() {
			playlistError(jobID, job, processInfo, playlistDir, fmt.Errorf("Download cancelled"), options.Recovering)
			return
		}
		if processInfo.IsFinishEarly() {
			log.Printf("[%s] Finishing early after %d videos", jobID, len(downloadedFiles))
			break
		}
		if !playlistHasDiskHeadroom() {
			for skippedIndex := i; skippedIndex < len(playlistInfo.Entries); skippedIndex++ {
				skippedEntry := playlistInfo.Entries[skippedIndex]
				failedVideos = append(failedVideos, services.FailedVideo{
					Num:    skippedIndex + 1,
					Title:  orDefault(skippedEntry.Title, fmt.Sprintf("Video %d", skippedIndex+1)),
					Reason: "Server storage limit reached before this item",
				})
			}
			job.Lock()
			job.FailedVideos = failedVideos
			job.FailedCount = len(failedVideos)
			job.Unlock()
			log.Printf("[%s] Preserving %d completed videos before disk reserve is exhausted", jobID, len(downloadedFiles))
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
			if err := checkpointActivePlaylist(jobID, job, options, videoNum+1); err != nil {
				log.Printf("[%s] Failed to checkpoint skipped playlist item: %v", jobID, err)
			}
			continue
		}

		filenamePreferences := options.DownloadPreferences
		filenamePreferences.Codec = options.EffectiveCodec(options.Container)
		safeTitle := util.SanitizeFilename(services.DownloadFilename(videoTitle, options.Quality, filenamePreferences))
		if len(safeTitle) > 100 {
			safeTitle = safeTitle[:100]
		}
		videoFile := filepath.Join(playlistDir, fmt.Sprintf("%03d - %s.%s", videoNum, safeTitle, options.OutputExt))

		progress := float64(videoNum-1) / float64(totalVideos) * 100
		job.Lock()
		job.CurrentVideo = videoNum
		job.CurrentVideoTitle = videoTitle
		job.Progress = progress
		job.Message = fmt.Sprintf("downloading %d/%d: %s", videoNum, totalVideos, videoTitle)
		job.Unlock()
		if err := checkpointActivePlaylist(jobID, job, options, videoNum); err != nil {
			log.Printf("[%s] Failed to checkpoint active playlist item: %v", jobID, err)
		}
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
					DownloadPreferences: options.DownloadPreferences,
					IsAudio:             options.IsAudio, Quality: options.Quality, Container: options.Container,
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
					return err
				} else {
					tempPath = result.Path
				}
				tempPath = result.Path
			} else {
				result, err := services.DownloadViaYtdlp(ctx, actualURL, fmt.Sprintf("temp_%d", videoNum), services.DownloadOpts{
					DownloadPreferences: options.DownloadPreferences,
					IsAudio:             options.IsAudio, Quality: options.Quality, Container: options.Container,
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
				playlistError(jobID, job, processInfo, playlistDir, downloadErr, options.Recovering)
				return
			}
			failedVideos = append(failedVideos, services.FailedVideo{Num: videoNum, Title: videoTitle, Reason: util.ToUserError(errMsg)})
			job.Lock()
			job.FailedVideos = failedVideos
			job.FailedCount = len(failedVideos)
			job.Unlock()
			if err := checkpointActivePlaylist(jobID, job, options, videoNum+1); err != nil {
				log.Printf("[%s] Failed to checkpoint failed playlist item: %v", jobID, err)
			}
			continue
		}

		if tempPath != "" {
			if _, err := os.Stat(tempPath); err == nil {
				processed, err := services.ProcessVideo(tempPath, videoFile, services.ProcessVideoOpts{
					DownloadPreferences: options.DownloadPreferences,
					IsAudio:             options.IsAudio, AudioFormat: options.AudioFormat, AudioBitrate: options.AudioBitrate, Container: options.Container, JobID: jobID,
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
				} else {
					failedVideos = append(failedVideos, services.FailedVideo{Num: videoNum, Title: videoTitle, Reason: util.ToUserError(err.Error())})
					job.Lock()
					job.FailedVideos = failedVideos
					job.FailedCount = len(failedVideos)
					job.Unlock()
				}
			} else {
				failedVideos = append(failedVideos, services.FailedVideo{Num: videoNum, Title: videoTitle, Reason: "Download failed"})
				job.Lock()
				job.FailedVideos = failedVideos
				job.FailedCount = len(failedVideos)
				job.Unlock()
			}
		} else {
			failedVideos = append(failedVideos, services.FailedVideo{Num: videoNum, Title: videoTitle, Reason: "Download failed"})
			job.Lock()
			job.FailedVideos = failedVideos
			job.FailedCount = len(failedVideos)
			job.Unlock()
		}
		if err := checkpointActivePlaylist(jobID, job, options, videoNum+1); err != nil {
			log.Printf("[%s] Failed to checkpoint completed playlist item: %v", jobID, err)
		}
	}

	if len(downloadedFiles) == 0 {
		playlistError(jobID, job, processInfo, playlistDir, fmt.Errorf("No videos were successfully downloaded"), options.Recovering)
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

	safePlaylistName := util.SanitizeFilename(orDefault(playlistTitle, "playlist"))

	zipPath, err := packagePlaylistFiles(jobID, playlistDir, downloadedFiles)
	if err != nil {
		playlistError(jobID, job, processInfo, playlistDir, fmt.Errorf("Failed to create zip: %v", err), options.Recovering)
		return
	}

	stat, err := os.Stat(zipPath)
	if err != nil {
		playlistError(jobID, job, processInfo, "", fmt.Errorf("zip file not found after creation"), options.Recovering)
		return
	}
	token := randomToken()
	fileName := safePlaylistName + ".zip"

	services.Global.SetBotDownload(token, &services.BotDownload{
		JobID:         jobID,
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
	job.OutputPath = zipPath
	job.OutputFilename = fileName
	job.MimeType = "application/zip"
	job.FailedVideos = failedVideos
	job.FailedCount = len(failedVideos)
	job.Unlock()
	if err := services.Global.PersistCompletedAsyncJob(jobID); err != nil {
		log.Printf("[%s] Failed to persist completed playlist: %v", jobID, err)
	}
	removeActivePlaylistCheckpoint(jobID)

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

func playlistHasDiskHeadroom() bool {
	return playlistDiskSpaceAvailableGB() >= float64(config.DiskSpaceMinGB)
}

func packagePlaylistFiles(jobID, playlistDir string, files []string) (string, error) {
	zipPath := filepath.Join(config.TempDirs["playlist"], fmt.Sprintf("%s.zip", jobID))
	stageDir := playlistZipStageDir()
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return "", err
	}
	stageFile, err := os.CreateTemp(stageDir, "yoink-playlist-"+jobID+"-*.zip")
	if err != nil {
		return "", err
	}
	stagePath := stageFile.Name()
	if err := stageFile.Close(); err != nil {
		os.Remove(stagePath)
		return "", err
	}
	if err := playlistZipWriter(stagePath, files); err != nil {
		os.Remove(stagePath)
		return "", err
	}
	os.RemoveAll(playlistDir)
	if err := playlistRenameFile(stagePath, zipPath); err == nil {
		return zipPath, nil
	}
	if err := copyPlaylistZip(stagePath, zipPath); err != nil {
		os.Remove(zipPath)
		return stagePath, nil
	}
	os.Remove(stagePath)
	return zipPath, nil
}

func copyPlaylistZip(sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, source)
	closeErr := destination.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func playlistError(jobID string, job *services.AsyncJob, processInfo *services.ProcessInfo, playlistDir string, err error, preserveRecovery bool) {
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
	if preserveRecovery {
		return
	}
	removeActivePlaylistCheckpoint(jobID)
	os.RemoveAll(playlistDir)
}

func handlePlaylistStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	job := services.Global.GetAsyncJob(jobID)
	if job == nil {
		job = services.Global.RestoreCompletedAsyncJob(jobID)
	}
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
		data = services.Global.RestoreCompletedAsyncJobByToken(token)
	}
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
		if data.JobID != "" {
			services.Global.DeleteAsyncJob(data.JobID)
			services.Global.RemoveCompletedAsyncJob(data.JobID)
		}
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
