package routes

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
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

func BotRoutes(r chi.Router) {
	r.Post("/api/bot/download", handleBotDownload)
	r.Post("/api/bot/download-playlist", handleBotDownloadPlaylist)
	r.Post("/api/bot/convert", handleBotConvert)
	r.Post("/api/bot/compress", handleBotCompressEndpoint)
	r.Get("/api/bot/status/{jobId}", handleBotStatus)
	r.Get("/api/download/{token}", handleDownloadPage)
	r.Get("/api/bot/download/{token}", handleBotFileDownload)
}

func checkBotAuth(r *http.Request) bool {
	if config.BotSecret == "" {
		return false
	}
	auth := r.Header.Get("Authorization")
	expected := "Bearer " + config.BotSecret
	return subtle.ConstantTimeCompare([]byte(auth), []byte(expected)) == 1
}

func handleBotDownload(w http.ResponseWriter, r *http.Request) {
	if !checkBotAuth(r) {
		respondJSON(w, 401, map[string]string{"error": "Unauthorized"})
		return
	}

	var body struct {
		URL         string `json:"url"`
		Format      string `json:"format"`
		Quality     string `json:"quality"`
		Container   string `json:"container"`
		AudioFormat string `json:"audioFormat"`
		Playlist    bool   `json:"playlist"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if body.URL == "" {
		respondJSON(w, 400, map[string]string{"error": "URL required"})
		return
	}
	check := util.ValidateURL(body.URL)
	if !check.Valid {
		respondJSON(w, 400, map[string]string{"error": check.Error})
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

	jobID := uuid.New().String()
	isAudio := body.Format == "audio"
	outputExt := body.Container
	if isAudio {
		outputExt = body.AudioFormat
	}

	job := &services.AsyncJob{
		Status:    "starting",
		Message:   "Initializing download...",
		CreatedAt: time.Now(),
		URL:       body.URL,
		Format:    outputExt,
	}
	services.Global.SetAsyncJob(jobID, job)
	respondJSON(w, 200, map[string]string{"jobId": jobID})

	go processBotDownload(jobID, job, body.URL, isAudio, body.AudioFormat, outputExt, body.Quality, body.Container, body.Playlist)
}

func processBotDownload(jobID string, job *services.AsyncJob, rawURL string, isAudio bool, audioFormat, outputExt, quality, container string, playlist bool) {
	ctx := context.Background()
	job.Lock()
	job.Status = "downloading"
	job.Message = "Downloading from source..."
	job.Unlock()

	isYouTube := strings.Contains(rawURL, "youtube.com") || strings.Contains(rawURL, "youtu.be")
	var downloadedPath, downloadedExt string

	if isYouTube {
		isClip := strings.Contains(rawURL, "/clip/")
		if isClip {
			job.SetMessage("Parsing clip...")
			clipData, err := services.ParseYouTubeClip(ctx, rawURL)
			if err != nil {
				botError(jobID, job, err)
				return
			}
			job.SetMessage("Downloading clip...")
			result, err := services.HandleClipDownload(ctx, clipData, jobID, config.TempDirs["bot"], func(progress float64, _, _ string) {
				job.SetProgressAndMessage(progress, fmt.Sprintf("Trimming... %.0f%%", progress))
			})
			if err != nil {
				botError(jobID, job, err)
				return
			}
			downloadedPath = result.Path
			downloadedExt = result.Ext
			job.SetProgress(100)
		} else {
			job.SetMessage("Downloading via yt-dlp...")
			botProgress := func(progress float64, speed, eta string) {
				job.Lock()
				job.Progress = progress
				job.Message = fmt.Sprintf("Downloading... %.0f%%", progress)
				job.Speed = speed
				job.ETA = eta
				job.Unlock()
			}
			result, err := services.DownloadViaYtdlp(ctx, rawURL, jobID, services.DownloadOpts{
				IsAudio: isAudio, AudioFormat: audioFormat, Quality: quality, Container: container,
				TempDir: config.TempDirs["bot"], FilePrefix: "bot-", Playlist: playlist, UseProxy: false,
				OnProgress: botProgress,
			})
			if err != nil {
				// Clean up partial files from failed attempt
				if entries, cleanErr := os.ReadDir(config.TempDirs["bot"]); cleanErr == nil {
					for _, e := range entries {
						if strings.HasPrefix(e.Name(), "bot-"+jobID) {
							os.Remove(filepath.Join(config.TempDirs["bot"], e.Name()))
						}
					}
				}
				if util.HasProxy() {
					log.Printf("[Bot] yt-dlp failed, retrying with proxy: %s", err)
					job.Lock()
					job.Message = "Retrying with proxy..."
					job.Progress = 0
					job.Speed = ""
					job.ETA = ""
					job.Unlock()
					result, err = services.DownloadViaYtdlp(ctx, rawURL, jobID, services.DownloadOpts{
						IsAudio: isAudio, AudioFormat: audioFormat, Quality: quality, Container: container,
						TempDir: config.TempDirs["bot"], FilePrefix: "bot-", Playlist: playlist, UseProxy: true,
						OnProgress: botProgress,
					})
				}
			}
			if err != nil {
				log.Printf("[Bot] yt-dlp with proxy failed, falling back to Cobalt: %s", err)
				job.SetMessage("Downloading via Cobalt...")
				cobaltResult, cobaltErr := services.DownloadViaCobalt(ctx, rawURL, jobID, isAudio, func(progress float64, _, _ int64) {
					job.SetProgress(progress)
				}, services.CobaltDownloadOpts{})
				if cobaltErr != nil {
					botError(jobID, job, cobaltErr)
					return
				}
				downloadedPath = cobaltResult.FilePath
				downloadedExt = cobaltResult.Ext
			} else {
				downloadedPath = result.Path
				downloadedExt = result.Ext
			}
			job.SetProgress(100)
		}
	} else {
		result, err := services.DownloadViaYtdlp(ctx, rawURL, jobID, services.DownloadOpts{
			IsAudio: isAudio, AudioFormat: audioFormat, Quality: quality, Container: container,
			TempDir: config.TempDirs["bot"], FilePrefix: "bot-", Playlist: playlist,
			OnProgress: func(progress float64, speed, eta string) {
				job.Lock()
				job.Progress = progress
				job.Message = fmt.Sprintf("Downloading... %.0f%%", progress)
				job.Speed = speed
				job.ETA = eta
				job.Unlock()
			},
		})
		if err != nil {
			botError(jobID, job, err)
			return
		}
		downloadedPath = result.Path
		downloadedExt = result.Ext
	}

	if downloadedPath == "" {
		botError(jobID, job, fmt.Errorf("Downloaded file not found"))
		return
	}

	job.Lock()
	job.Status = "processing"
	job.Progress = 100
	job.Message = "Processing..."
	job.Unlock()

	finalFile := filepath.Join(config.TempDirs["bot"], fmt.Sprintf("bot-%s-final.%s", jobID, outputExt))
	processed, err := services.ProcessVideo(downloadedPath, finalFile, services.ProcessVideoOpts{
		IsAudio: isAudio, AudioFormat: audioFormat, Container: container, JobID: jobID,
	})
	if err != nil {
		botError(jobID, job, err)
		return
	}

	actualFinalFile := finalFile
	if processed.Skipped {
		actualFinalFile = processed.Path
	} else {
		os.Remove(downloadedPath)
	}

	stat, err := os.Stat(actualFinalFile)
	if err != nil {
		botError(jobID, job, fmt.Errorf("Downloaded file not found after processing"))
		return
	}

	token := makeBotToken()

	title := "download"
	isYT := strings.Contains(rawURL, "youtube.com") || strings.Contains(rawURL, "youtu.be")
	args := append([]string{}, util.GetYouTubeAuthArgs()...)
	if isYT {
		args = append(args, util.GetProxyArgs()...)
	}
	args = append(args, "--print", "title", "--no-playlist", rawURL)
	if out, err := exec.Command("yt-dlp", args...).Output(); err == nil {
		t := strings.TrimSpace(string(out))
		if len(t) > 100 {
			t = t[:100]
		}
		if t != "" {
			title = t
		}
	}

	ext := outputExt
	if processed.Skipped {
		ext = downloadedExt
	}
	fileName := util.SanitizeFilename(title) + "." + ext

	mimeType := "video/mp4"
	if isAudio {
		if m, ok := config.AudioMIMEs[audioFormat]; ok {
			mimeType = m
		} else {
			mimeType = "audio/mpeg"
		}
	} else {
		if m, ok := config.ContainerMIMEs[container]; ok {
			mimeType = m
		}
	}

	services.Global.SetBotDownload(token, &services.BotDownload{
		FilePath:  actualFinalFile,
		FileName:  fileName,
		FileSize:  stat.Size(),
		MimeType:  mimeType,
		CreatedAt: time.Now(),
	})

	job.Lock()
	job.Status = "complete"
	job.Progress = 100
	job.Message = "Ready for download"
	job.FileName = fileName
	job.FileSize = stat.Size()
	job.DownloadToken = token
	job.Unlock()

	log.Printf("[Bot] Job %s complete, token: %s...", jobID, token[:8])
}

func botError(jobID string, job *services.AsyncJob, err error) {
	log.Printf("[Bot] Job %s failed: %s", jobID, err)
	alerts.BotJobFailed(jobID, job.URL, err)
	job.Lock()
	job.Status = "error"
	job.Message = util.ToUserError(err.Error())
	job.DebugError = err.Error()
	job.Unlock()

	entries, _ := os.ReadDir(config.TempDirs["bot"])
	for _, e := range entries {
		if strings.Contains(e.Name(), jobID) {
			os.Remove(filepath.Join(config.TempDirs["bot"], e.Name()))
		}
	}
}

func handleBotDownloadPlaylist(w http.ResponseWriter, r *http.Request) {
	if !checkBotAuth(r) {
		respondJSON(w, 401, map[string]string{"error": "Unauthorized"})
		return
	}

	var body struct {
		URL          string `json:"url"`
		Format       string `json:"format"`
		Quality      string `json:"quality"`
		Container    string `json:"container"`
		AudioFormat  string `json:"audioFormat"`
		AudioBitrate string `json:"audioBitrate"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if body.URL == "" {
		respondJSON(w, 400, map[string]string{"error": "URL required"})
		return
	}
	check := util.ValidateURL(body.URL)
	if !check.Valid {
		respondJSON(w, 400, map[string]string{"error": check.Error})
		return
	}

	defaults(&body.Format, "video")
	defaults(&body.Quality, "1080p")
	defaults(&body.Container, "mp4")
	defaults(&body.AudioFormat, "mp3")
	defaults(&body.AudioBitrate, "320")

	jobID := uuid.New().String()
	isAudio := body.Format == "audio"
	outputExt := body.Container
	if isAudio {
		outputExt = body.AudioFormat
	}

	job := &services.AsyncJob{
		Status:    "starting",
		Message:   "Getting playlist info...",
		CreatedAt: time.Now(),
		URL:       body.URL,
		Format:    outputExt,
	}
	services.Global.SetAsyncJob(jobID, job)
	respondJSON(w, 200, map[string]string{"jobId": jobID})

	go processBotPlaylistAsync(jobID, job, body.URL, isAudio, body.AudioFormat, outputExt, body.Quality, body.Container, body.AudioBitrate)
}

func processBotPlaylistAsync(jobID string, job *services.AsyncJob, rawURL string, isAudio bool, audioFormat, outputExt, quality, container, audioBitrate string) {
	playlistDir := filepath.Join(config.TempDirs["bot"], "playlist-"+jobID)
	os.MkdirAll(playlistDir, 0755)
	ctx := context.Background()

	isYT := strings.Contains(rawURL, "youtube.com") || strings.Contains(rawURL, "youtu.be")
	playlistInfo, err := services.GetPlaylistInfo(ctx, rawURL, isYT)
	if err != nil {
		botError(jobID, job, err)
		os.RemoveAll(playlistDir)
		return
	}

	if playlistInfo.Count > config.MaxPlaylistVideos {
		botError(jobID, job, fmt.Errorf("Playlist too large. Maximum %d videos allowed.", config.MaxPlaylistVideos))
		os.RemoveAll(playlistDir)
		return
	}

	job.Lock()
	job.TotalVideos = playlistInfo.Count
	job.PlaylistInfo = map[string]interface{}{"title": playlistInfo.Title, "count": playlistInfo.Count}
	job.Message = fmt.Sprintf("Found %d videos", playlistInfo.Count)
	job.Status = "downloading"
	job.Unlock()

	var downloadedFiles []string
	var failedVideos []services.FailedVideo

	for i, entry := range playlistInfo.Entries {
		videoNum := i + 1
		videoTitle := orDefault(entry.Title, fmt.Sprintf("Video %d", videoNum))
		videoURL := entry.URL
		if videoURL == "" && entry.ID != "" {
			videoURL = "https://www.youtube.com/watch?v=" + entry.ID
		}
		if videoURL == "" {
			continue
		}

		job.SetProgressAndMessage(
			float64(videoNum)/float64(playlistInfo.Count)*90,
			fmt.Sprintf("Downloading %d/%d: %s", videoNum, playlistInfo.Count, videoTitle))

		safeTitle := util.SanitizeFilename(videoTitle)
		if len(safeTitle) > 100 {
			safeTitle = safeTitle[:100]
		}
		videoFile := filepath.Join(playlistDir, fmt.Sprintf("%03d - %s.%s", videoNum, safeTitle, outputExt))

		var tempPath string
		isYTVideo := strings.Contains(videoURL, "youtube.com") || strings.Contains(videoURL, "youtu.be")

		result, dlErr := services.DownloadViaYtdlp(ctx, videoURL, fmt.Sprintf("temp_%d", videoNum), services.DownloadOpts{
			IsAudio: isAudio, AudioFormat: audioFormat, Quality: quality, Container: container,
			TempDir: playlistDir, UseProxy: false,
		})
		if dlErr != nil && isYTVideo && util.HasProxy() {
			// Clean up partial files from failed attempt
			prefix := fmt.Sprintf("temp_%d", videoNum)
			if entries, cleanErr := os.ReadDir(playlistDir); cleanErr == nil {
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), prefix) {
						os.Remove(filepath.Join(playlistDir, e.Name()))
					}
				}
			}
			result, dlErr = services.DownloadViaYtdlp(ctx, videoURL, fmt.Sprintf("temp_%d", videoNum), services.DownloadOpts{
				IsAudio: isAudio, AudioFormat: audioFormat, Quality: quality, Container: container,
				TempDir: playlistDir, UseProxy: true,
			})
		}
		if dlErr != nil && isYTVideo {
			cobaltResult, cobaltErr := services.DownloadViaCobalt(ctx, videoURL, fmt.Sprintf("%s-v%d", jobID, videoNum), isAudio, nil,
				services.CobaltDownloadOpts{OutputDir: playlistDir, MaxRetries: 2, RetryDelay: time.Second})
			if cobaltErr != nil {
				failedVideos = append(failedVideos, services.FailedVideo{Num: videoNum, Title: videoTitle, Reason: util.ToUserError(cobaltErr.Error())})
				job.Lock()
				job.FailedVideos = failedVideos
				job.Unlock()
				continue
			}
			tempPath = cobaltResult.FilePath
		} else if dlErr != nil {
			failedVideos = append(failedVideos, services.FailedVideo{Num: videoNum, Title: videoTitle, Reason: util.ToUserError(dlErr.Error())})
			job.Lock()
			job.FailedVideos = failedVideos
			job.Unlock()
			continue
		} else {
			tempPath = result.Path
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
				}
			}
		}
	}

	if len(downloadedFiles) == 0 {
		botError(jobID, job, fmt.Errorf("No videos were successfully downloaded"))
		os.RemoveAll(playlistDir)
		return
	}

	job.SetProgressAndMessage(95, "Creating zip file...")

	zipPath := filepath.Join(config.TempDirs["bot"], fmt.Sprintf("playlist-%s.zip", jobID))
	safePlaylistName := util.SanitizeFilename(orDefault(playlistInfo.Title, "playlist"))
	if err := createZip(zipPath, downloadedFiles); err != nil {
		botError(jobID, job, err)
		os.RemoveAll(playlistDir)
		return
	}

	stat, err := os.Stat(zipPath)
	if err != nil {
		botError(jobID, job, fmt.Errorf("zip file not found after creation"))
		os.RemoveAll(playlistDir)
		return
	}
	token := makeBotToken()
	fileName := safePlaylistName + ".zip"

	services.Global.SetBotDownload(token, &services.BotDownload{
		FilePath:   zipPath,
		FileName:   fileName,
		FileSize:   stat.Size(),
		MimeType:   "application/zip",
		CreatedAt:  time.Now(),
		IsPlaylist: true,
	})

	job.Lock()
	job.Status = "complete"
	job.Progress = 100
	job.Message = fmt.Sprintf("Ready for download (%d videos)", len(downloadedFiles))
	job.FileName = fileName
	job.FileSize = stat.Size()
	job.DownloadToken = token
	job.VideosCompleted = len(downloadedFiles)
	job.Unlock()

	log.Printf("[Bot] Playlist job %s complete, token: %s...", jobID, token[:8])
	os.RemoveAll(playlistDir)
}

func handleBotConvert(w http.ResponseWriter, r *http.Request) {
	if !checkBotAuth(r) {
		respondJSON(w, 401, map[string]string{"error": "Unauthorized"})
		return
	}

	var body struct {
		URL    string `json:"url"`
		Format string `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, 400, map[string]string{"error": "Invalid JSON body"})
		return
	}

	if body.URL == "" {
		respondJSON(w, 400, map[string]string{"error": "URL required"})
		return
	}
	format := body.Format
	if format == "" {
		format = "mp4"
	}
	if !config.Contains(config.AllowedFormats, format) {
		respondJSON(w, 400, map[string]string{"error": "Invalid format"})
		return
	}

	jobID := uuid.New().String()
	job := &services.AsyncJob{
		Status:    "processing",
		Progress:  0,
		Message:   "Downloading file...",
		CreatedAt: time.Now(),
	}
	services.Global.SetAsyncJob(jobID, job)
	respondJSON(w, 200, map[string]string{"jobId": jobID})

	go func() {
		tempPath, originalName, err := downloadURLToTemp(body.URL, jobID)
		if err != nil {
			log.Printf("[BotConvert] Download failed: %s", err)
			alerts.ConversionFailed(jobID, format, err)
			job.SetError("Failed to download file: " + err.Error())
			return
		}

		job.SetProgressAndMessage(20, "Converting...")

		isAudio := isAudioFmt(format)
		outputPath := filepath.Join(config.TempDirs["convert"], jobID+"-converted."+format)

		convertCheck := services.Global.CanStartJob("convert")
		if !convertCheck.OK {
			os.Remove(tempPath)
			job.SetError(convertCheck.Reason)
			return
		}

		processInfo := &services.ProcessInfo{TempFile: outputPath, JobType: "convert"}
		services.Global.SetProcess(jobID, processInfo)

		var result *services.ProcessResult
		if isAudio {
			result, err = services.ProcessVideo(tempPath, outputPath, services.ProcessVideoOpts{
				IsAudio: true, AudioFormat: format, AudioBitrate: "320", JobID: jobID,
			})
		} else {
			result, err = services.ProcessVideo(tempPath, outputPath, services.ProcessVideoOpts{
				Container: format, JobID: jobID,
			})
		}

		if err != nil {
			os.Remove(tempPath)
			os.Remove(outputPath)
			services.Global.ReleaseJob(jobID)
			alerts.ConversionFailed(jobID, format, err)
			job.SetError("Conversion failed: " + err.Error())
			return
		}

		actualOutput := outputPath
		if result.Skipped {
			actualOutput = tempPath
		} else {
			os.Remove(tempPath)
		}

		stat, err := os.Stat(actualOutput)
		if err != nil {
			services.Global.ReleaseJob(jobID)
			job.SetError("Output file not found")
			return
		}

		token := makeBotToken()
		baseName := strings.TrimSuffix(originalName, filepath.Ext(originalName))
		outputFilename := util.SanitizeFilename(baseName) + "." + format

		mimeType := services.GetMimeType(format, isAudio, false)

		services.Global.SetBotDownload(token, &services.BotDownload{
			FilePath:  actualOutput,
			FileName:  outputFilename,
			FileSize:  stat.Size(),
			MimeType:  mimeType,
			CreatedAt: time.Now(),
		})

		job.Lock()
		job.Status = "complete"
		job.Progress = 100
		job.Message = "Conversion complete"
		job.FileName = outputFilename
		job.FileSize = stat.Size()
		job.DownloadToken = token
		job.OutputPath = actualOutput
		job.OutputFilename = outputFilename
		job.MimeType = mimeType
		job.Unlock()

		services.Global.ReleaseJob(jobID)
		log.Printf("[BotConvert] Job %s complete: %s", jobID, outputFilename)
	}()
}

func handleBotCompressEndpoint(w http.ResponseWriter, r *http.Request) {
	if !checkBotAuth(r) {
		respondJSON(w, 401, map[string]string{"error": "Unauthorized"})
		return
	}

	var body struct {
		URL           string `json:"url"`
		DownloadToken string `json:"downloadToken"`
		TargetSize    string `json:"targetSize"`
		Preset        string `json:"preset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, 400, map[string]string{"error": "Invalid JSON body"})
		return
	}

	if body.URL == "" && body.DownloadToken == "" {
		respondJSON(w, 400, map[string]string{"error": "URL or downloadToken required"})
		return
	}

	targetSize := body.TargetSize
	if targetSize == "" {
		targetSize = "25"
	}
	preset := body.Preset
	if preset == "" {
		preset = "fast"
	}
	if !config.Contains(config.AllowedPresets, preset) {
		preset = "fast"
	}

	jobID := uuid.New().String()
	job := &services.AsyncJob{
		Status:    "processing",
		Progress:  0,
		Message:   "Preparing compression...",
		CreatedAt: time.Now(),
	}
	services.Global.SetAsyncJob(jobID, job)
	respondJSON(w, 200, map[string]string{"jobId": jobID})

	go func() {
		var inputPath, originalName string

		if body.DownloadToken != "" {
			dl := services.Global.GetBotDownload(body.DownloadToken)
			if dl == nil {
				job.SetError("Download token not found or expired")
				return
			}
			inputPath = dl.FilePath
			originalName = dl.FileName
		} else {
			var err error
			inputPath, originalName, err = downloadURLToTemp(body.URL, jobID)
			if err != nil {
				log.Printf("[BotCompress] Download failed: %s", err)
				alerts.CompressionFailed(jobID, err)
				job.SetError("Failed to download file: " + err.Error())
				return
			}
		}

		probe := probeVideoFull(inputPath)
		durationStr := fmt.Sprintf("%.2f", probe.duration)

		err := handleCompressAsync(inputPath, originalName, "", targetSize, durationStr,
			"size", "medium", preset, "auto", false, jobID)
		if err != nil {
			log.Printf("[BotCompress] Job %s failed: %s", jobID, err)
			alerts.CompressionFailed(jobID, err)
			job.SetError(err.Error())
			return
		}

		completedJob := services.Global.GetAsyncJob(jobID)
		if completedJob == nil {
			return
		}

		outputPath, outputFilename, mimeType, status := completedJob.GetDownloadInfo()
		if status != "complete" || outputPath == "" {
			return
		}

		stat, err := os.Stat(outputPath)
		if err != nil {
			return
		}

		token := makeBotToken()
		services.Global.SetBotDownload(token, &services.BotDownload{
			FilePath:  outputPath,
			FileName:  outputFilename,
			FileSize:  stat.Size(),
			MimeType:  mimeType,
			CreatedAt: time.Now(),
		})

		completedJob.Lock()
		completedJob.FileName = outputFilename
		completedJob.FileSize = stat.Size()
		completedJob.DownloadToken = token
		completedJob.Unlock()

		log.Printf("[BotCompress] Job %s complete: %s (%.2fMB)", jobID, outputFilename, float64(stat.Size())/(1024*1024))
	}()
}

func downloadURLToTemp(rawURL, jobID string) (string, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid URL: %w", err)
	}
	allowedHosts := []string{"cdn.discordapp.com", "media.discordapp.net"}
	hostAllowed := false
	for _, h := range allowedHosts {
		if parsed.Host == h || strings.HasSuffix(parsed.Host, "."+h) {
			hostAllowed = true
			break
		}
	}
	if !hostAllowed {
		return "", "", fmt.Errorf("only Discord CDN URLs are accepted")
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("URL returned HTTP %d", resp.StatusCode)
	}

	originalName := "file"
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		_, params, parseErr := mime.ParseMediaType(cd)
		if parseErr == nil {
			if name, ok := params["filename"]; ok && name != "" {
				originalName = name
			}
		}
	}
	if originalName == "file" {
		parts := strings.Split(strings.Split(rawURL, "?")[0], "/")
		if len(parts) > 0 {
			last := parts[len(parts)-1]
			if last != "" {
				originalName = last
			}
		}
	}

	ext := filepath.Ext(originalName)
	if ext == "" {
		ext = ".mp4"
	}

	tempPath := filepath.Join(config.TempDirs["bot"], fmt.Sprintf("bot-%s-upload%s", jobID, ext))
	f, err := os.Create(tempPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to create temp file: %w", err)
	}

	written, copyErr := io.Copy(f, io.LimitReader(resp.Body, config.FileSizeLimit))
	if closeErr := f.Close(); closeErr != nil && copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		os.Remove(tempPath)
		return "", "", fmt.Errorf("failed to write file: %w", copyErr)
	}

	if written == 0 {
		os.Remove(tempPath)
		return "", "", fmt.Errorf("downloaded file is empty")
	}

	return tempPath, originalName, nil
}

func handleBotStatus(w http.ResponseWriter, r *http.Request) {
	if !checkBotAuth(r) {
		respondJSON(w, 401, map[string]string{"error": "Unauthorized"})
		return
	}

	jobID := chi.URLParam(r, "jobId")
	job := services.Global.GetAsyncJob(jobID)
	if job == nil {
		respondJSON(w, 404, map[string]string{"error": "Job not found"})
		return
	}

	respondJSON(w, 200, job.GetBotStatus())
}

func handleDownloadPage(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	data := services.Global.GetBotDownload(token)
	if data == nil {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(404)
		fmt.Fprint(w, botNotFoundHTML)
		return
	}
	downloadURL := "/api/bot/download/" + token
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, botDownloadHTML, html.EscapeString(data.FileName), html.EscapeString(downloadURL))
}

func handleBotFileDownload(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "File read error", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	io.Copy(w, f)

	data.Downloaded = true
	go func() {
		time.Sleep(30 * time.Second)
		if services.Global.GetBotDownload(token) != nil {
			os.Remove(data.FilePath)
			services.Global.DeleteBotDownload(token)
			log.Printf("[Bot] Token %s... cleaned up after download", token[:min(8, len(token))])
		}
	}()
}

func StartBotDownloadExpiry() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			now := time.Now()
			services.Global.ForEachBotDownload(func(token string, dl *services.BotDownload) bool {
				if now.Sub(dl.CreatedAt) > config.BotDownloadExpiry && !dl.IsWebPlaylist && !dl.IsPlaylist {
					short := token
					if len(short) > 8 {
						short = short[:8]
					}
					log.Printf("[Bot] Download token %s... expired", short)
					os.Remove(dl.FilePath)
					return true
				}
				return false
			})
		}
	}()
}

func StartPlaylistDownloadExpiry() {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			now := time.Now()
			services.Global.ForEachBotDownload(func(token string, dl *services.BotDownload) bool {
				if dl.IsWebPlaylist && now.Sub(dl.CreatedAt) > config.PlaylistDownloadExp {
					short := token
					if len(short) > 8 {
						short = short[:8]
					}
					log.Printf("[Playlist] Download token %s... expired", short)
					os.Remove(dl.FilePath)
					return true
				}
				return false
			})
		}
	}()
}

func makeBotToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func defaults(s *string, def string) {
	if *s == "" {
		*s = def
	}
}

const botNotFoundHTML = `<!DOCTYPE html><html><head><title>download not found</title><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin><link href="https://fonts.googleapis.com/css2?family=Montserrat:wght@800&family=Poppins:wght@400;500&display=swap" rel="stylesheet"><style>*{box-sizing:border-box;margin:0;padding:0}body{font-family:'Poppins',sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;background:#0a0a0f;color:#fafafa;padding:24px}.container{text-align:center;max-width:500px;width:100%}h1{font-family:'Montserrat',sans-serif;font-weight:800;font-size:2rem;margin-bottom:12px;letter-spacing:-0.03em;color:#f87171}p{color:#a1a1aa;font-size:1rem;margin-bottom:8px}.status{margin-top:24px;padding:16px;background:#12121a;border-radius:12px;border:1px solid #2a2a3a;font-size:0.9rem;color:#f87171}</style></head><body><div class="container"><h1>download failed</h1><p>this file is no longer available</p><div class="status">the download link has expired (5 minute limit)</div></div></body></html>`

const botDownloadHTML = `<!DOCTYPE html><html><head><title>downloading...</title><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin><link href="https://fonts.googleapis.com/css2?family=Montserrat:wght@800&family=Poppins:wght@400;500&display=swap" rel="stylesheet"><style>*{box-sizing:border-box;margin:0;padding:0}body{font-family:'Poppins',sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;background:#0a0a0f;color:#fafafa;padding:24px}.container{text-align:center;max-width:500px;width:100%%}.spinner-wrap{display:flex;justify-content:center;margin-bottom:24px}.spinner{width:60px;height:60px;border:4px solid #2a2a3a;border-top:4px solid #8b5cf6;border-radius:50%%;animation:spin 0.8s linear infinite}@keyframes spin{from{transform:rotate(0deg)}to{transform:rotate(360deg)}}h1{font-family:'Montserrat',sans-serif;font-weight:800;font-size:2rem;margin-bottom:12px;letter-spacing:-0.03em}p{color:#a1a1aa;font-size:1rem;margin-bottom:8px}.status{margin-top:24px;padding:16px;background:#12121a;border-radius:12px;border:1px solid #2a2a3a;font-size:0.9rem;color:#a1a1aa;word-break:break-all}.success h1{color:#4ade80}.success .status{color:#4ade80}</style></head><body><div class="container"><div class="spinner-wrap"><div class="spinner"></div></div><h1>downloading...</h1><p>your download should start automatically</p><div class="status">%s</div></div><iframe id="downloadFrame" style="display:none"></iframe><script>document.getElementById('downloadFrame').src='%s';setTimeout(()=>{window.close();setTimeout(()=>{document.body.innerHTML='<div class="container success"><h1>done</h1><p>download started successfully</p><div class="status">you can close this page now</div></div>'},100)},2000)</script></body></html>`
