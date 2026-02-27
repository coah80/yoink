package routes

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/coah80/yoink/internal/alerts"
	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
	"github.com/coah80/yoink/internal/util"
)

func DownloadRoutes(r chi.Router) {
	r.Get("/api/metadata", handleMetadata)
	r.Get("/api/download", handleDownload)
}

func handleMetadata(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	downloadPlaylist := r.URL.Query().Get("playlist") == "true"

	check := util.ValidateURL(rawURL)
	if !check.Valid {
		respondJSON(w, 400, map[string]string{"error": check.Error})
		return
	}

	isYouTube := strings.Contains(rawURL, "youtube.com") || strings.Contains(rawURL, "youtu.be")
	isClip := strings.Contains(rawURL, "/clip/")
	ctx := r.Context()

	if isClip {
		clipData, err := services.ParseYouTubeClip(ctx, rawURL)
		if err != nil {
			respondJSON(w, 200, map[string]interface{}{
				"isClip":       true,
				"title":        "YouTube Clip",
				"usingCookies": false,
				"clipNote":     "Clip will be downloaded via yt-dlp.",
			})
			return
		}

		clipDuration := float64(clipData.EndTimeMs-clipData.StartTimeMs) / 1000

		cobaltMeta, err := services.FetchMetadataViaCobalt(ctx, clipData.FullVideoURL)
		if err == nil {
			respondJSON(w, 200, map[string]interface{}{
				"title":            cobaltMeta.Title,
				"ext":              cobaltMeta.Ext,
				"id":               cobaltMeta.ID,
				"uploader":         cobaltMeta.Uploader,
				"duration":         clipDuration,
				"thumbnail":        cobaltMeta.Thumbnail,
				"isPlaylist":       false,
				"viaCobalt":        true,
				"isClip":           true,
				"clipStartTime":    float64(clipData.StartTimeMs) / 1000,
				"clipEndTime":      float64(clipData.EndTimeMs) / 1000,
				"clipDuration":     clipDuration,
				"originalVideoId":  clipData.VideoID,
				"originalDuration": cobaltMeta.Duration,
				"fullVideoUrl":     clipData.FullVideoURL,
				"usingCookies":     false,
				"clipNote":         "Clip will download full video then trim to clip portion.",
			})
			return
		}

		respondJSON(w, 200, map[string]interface{}{
			"isClip":          true,
			"clipStartTime":   float64(clipData.StartTimeMs) / 1000,
			"clipEndTime":     float64(clipData.EndTimeMs) / 1000,
			"clipDuration":    clipDuration,
			"duration":        clipDuration,
			"originalVideoId": clipData.VideoID,
			"fullVideoUrl":    clipData.FullVideoURL,
			"title":           "YouTube Clip",
			"thumbnail":       fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", clipData.VideoID),
			"usingCookies":    false,
			"clipNote":        "Clip will download full video then trim to clip portion.",
		})
		return
	}

	if isYouTube && !downloadPlaylist {
		cobaltMeta, err := services.FetchMetadataViaCobalt(ctx, rawURL)
		if err != nil {
			respondJSON(w, 500, map[string]string{"error": "Failed to fetch YouTube metadata via Cobalt"})
			return
		}
		result := map[string]interface{}{
			"title":      cobaltMeta.Title,
			"ext":        cobaltMeta.Ext,
			"id":         cobaltMeta.ID,
			"uploader":   cobaltMeta.Uploader,
			"duration":   cobaltMeta.Duration,
			"thumbnail":  cobaltMeta.Thumbnail,
			"isPlaylist": false,
			"viaCobalt":  true,
			"usingCookies": false,
		}
		respondJSON(w, 200, result)
		return
	}

	usingCookies := util.HasCookiesFile()
	isYouTubeURL := strings.Contains(rawURL, "youtube.com") || strings.Contains(rawURL, "youtu.be")

	args := append([]string{}, util.GetYouTubeAuthArgs()...)
	if isYouTubeURL {
		args = append(args, util.GetProxyArgs()...)
	}
	args = append(args, "-t", "sleep")

	if !downloadPlaylist {
		args = append(args, "--no-playlist",
			"--print", "%(title)s", "--print", "%(ext)s", "--print", "%(id)s",
			"--print", "%(uploader)s", "--print", "%(duration)s", "--print", "%(thumbnail)s",
			rawURL,
		)
	} else {
		args = append(args, "--yes-playlist", "--flat-playlist",
			"--print", "%(playlist_title)s", "--print", "%(playlist_count)s", "--print", "%(title)s",
			rawURL,
		)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "yt-dlp", args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		respondJSON(w, 500, map[string]string{"error": "yt-dlp not found. Please install yt-dlp."})
		return
	}

	var outBuf, errBuf strings.Builder
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			outBuf.WriteString(scanner.Text() + "\n")
		}
	}()
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			errBuf.WriteString(scanner.Text() + "\n")
		}
	}()

	err := cmd.Wait()
	if err != nil {
		if cmdCtx.Err() != nil {
			respondJSON(w, 504, map[string]string{"error": "Metadata fetch timed out (30s)"})
			return
		}
		errOutput := errBuf.String()
		if util.NeedsCookiesRetry(errOutput) && !usingCookies {
			alerts.CookieIssue("YouTube bot detection - cookies.txt may be stale or missing")
			respondJSON(w, 500, map[string]string{"error": "YouTube requires authentication. Please add cookies.txt to the server."})
			return
		}
		if !downloadPlaylist {
			galleryMeta, galleryErr := tryGalleryDlMetadata(ctx, rawURL)
			if galleryErr == nil {
				respondJSON(w, 200, galleryMeta)
				return
			}
			log.Printf("[Metadata] gallery-dl fallback also failed: %s", galleryErr)
		}
		respondJSON(w, 500, map[string]string{"error": util.ToUserError(errOutput)})
		return
	}

	lines := strings.Split(strings.TrimSpace(outBuf.String()), "\n")
	if downloadPlaylist {
		playlistTitle := "Playlist"
		if len(lines) > 0 {
			playlistTitle = lines[0]
		}
		videoCount := 0
		if len(lines) > 1 {
			fmt.Sscanf(lines[1], "%d", &videoCount)
		}
		videoTitles := []string{}
		if len(lines) > 2 {
			for _, t := range lines[2:] {
				t = strings.TrimSpace(t)
				if t != "" {
					videoTitles = append(videoTitles, t)
				}
			}
		}
		if videoCount == 0 {
			videoCount = len(videoTitles)
		}
		cap := 50
		if len(videoTitles) < cap {
			cap = len(videoTitles)
		}
		respondJSON(w, 200, map[string]interface{}{
			"title":        playlistTitle,
			"isPlaylist":   true,
			"videoCount":   videoCount,
			"videoTitles":  videoTitles[:cap],
			"usingCookies": usingCookies,
		})
	} else {
		get := func(i int) string {
			if i < len(lines) {
				return lines[i]
			}
			return ""
		}
		respondJSON(w, 200, map[string]interface{}{
			"title":        orDefault(get(0), "download"),
			"ext":          orDefault(get(1), "mp4"),
			"id":           get(2),
			"uploader":     get(3),
			"duration":     get(4),
			"thumbnail":    get(5),
			"isPlaylist":   false,
			"usingCookies": usingCookies,
		})
	}
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rawURL := q.Get("url")
	format := orDefault(q.Get("format"), "video")
	filename := q.Get("filename")
	quality := orDefault(q.Get("quality"), "1080p")
	container := orDefault(q.Get("container"), "mp4")
	audioFormat := orDefault(q.Get("audioFormat"), "mp3")
	audioBitrate := orDefault(q.Get("audioBitrate"), "320")
	progressID := q.Get("progressId")
	clientID := q.Get("clientId")
	twitterGifs := q.Get("twitterGifs") != "false"
	downloadPlaylist := q.Get("playlist") == "true"

	downloadID := progressID
	if downloadID == "" {
		downloadID = uuid.New().String()
	}

	check := util.ValidateURL(rawURL)
	if !check.Valid {
		respondJSON(w, 400, map[string]string{"error": check.Error})
		return
	}

	if clientID != "" {
		if services.Global.GetClientJobCount(clientID) >= config.MaxJobsPerClient {
			services.Global.SendProgressSimple(downloadID, "error", fmt.Sprintf("Too many active jobs. Maximum %d concurrent jobs per user.", config.MaxJobsPerClient))
			respondJSON(w, 429, map[string]string{"error": fmt.Sprintf("Too many active jobs. Maximum %d concurrent jobs per user.", config.MaxJobsPerClient)})
			return
		}
	}

	jobCheck := services.Global.CanStartJob("download")
	if !jobCheck.OK {
		services.Global.SendProgressSimple(downloadID, "error", jobCheck.Reason)
		respondJSON(w, 503, map[string]string{"error": jobCheck.Reason})
		return
	}
	if clientID != "" {
		services.Global.RegisterClient(clientID)
		services.Global.LinkJobToClient(downloadID, clientID)
	}

	isAudio := format == "audio"
	outputExt := container
	if isAudio {
		outputExt = audioFormat
	}
	finalFile := filepath.Join(config.TempDirs["download"], fmt.Sprintf("%s-final.%s", downloadID, outputExt))

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	processInfo := &services.ProcessInfo{
		TempFile: finalFile,
		JobType:  "download",
		CancelFunc: cancel,
	}
	services.Global.SetProcess(downloadID, processInfo)

	services.Global.RegisterPendingJob(downloadID, &services.PendingJob{
		Type:     "download",
		URL:      rawURL,
		ClientID: clientID,
		Status:   "starting",
	})

	jobs := services.Global.GetJobsByType()
	jobsJSON, _ := json.Marshal(jobs)
	log.Printf("[Queue] Download started. Active: %s", jobsJSON)
	services.Global.SendProgressSimple(downloadID, "starting", "Initializing download...")

	isYouTube := strings.Contains(rawURL, "youtube.com") || strings.Contains(rawURL, "youtu.be")

	go func() {
		<-r.Context().Done()
		if !processInfo.IsCancelled() {
			processInfo.SetCancelled(true)
			cancel()
			processInfo.KillProcess()
		}
	}()

	services.Global.SendProgressWithPercent(downloadID, "downloading", "Downloading from source...", 0)

	var downloadedPath, downloadedExt string

	if isYouTube && !downloadPlaylist {
		isClip := strings.Contains(rawURL, "/clip/")

		if isClip {
			clipData, err := services.ParseYouTubeClip(ctx, rawURL)
			if err != nil {
				handleDownloadError(w, downloadID, outputExt, err)
				return
			}
			services.Global.SendProgressWithPercent(downloadID, "downloading", "Trimming clip from stream...", 0)
			result, err := services.HandleClipDownload(ctx, clipData, downloadID, config.TempDirs["download"], func(progress float64, speed, eta string) {
				services.Global.SendProgress(downloadID, "downloading", fmt.Sprintf("Trimming... %.0f%%", progress), &progress, map[string]interface{}{"speed": speed, "eta": eta})
				services.Global.UpdatePendingJob(downloadID, progress, "downloading")
			})
			if err != nil {
				handleDownloadError(w, downloadID, outputExt, err)
				return
			}
			downloadedPath = result.Path
			downloadedExt = result.Ext
		} else {
			services.Global.SendProgressWithPercent(downloadID, "downloading", "Downloading via yt-dlp...", 0)
			result, err := services.DownloadViaYtdlp(ctx, rawURL, downloadID, services.DownloadOpts{
				IsAudio:     isAudio,
				AudioFormat: audioFormat,
				Quality:     quality,
				Container:   container,
				TempDir:     config.TempDirs["download"],
				ProcessInfo: processInfo,
				Playlist:    false,
				UseProxy:    true,
				OnProgress: func(progress float64, speed, eta string) {
					services.Global.SendProgress(downloadID, "downloading", fmt.Sprintf("Downloading... %.0f%%", progress), &progress, map[string]interface{}{"speed": speed, "eta": eta})
					services.Global.UpdatePendingJob(downloadID, progress, "downloading")
				},
			})
			if err != nil {
				log.Printf("[%s] yt-dlp failed, falling back to Cobalt: %s", downloadID, err)
				services.Global.SendProgressWithPercent(downloadID, "downloading", "Downloading via Cobalt...", 0)
				cobaltResult, cobaltErr := services.DownloadViaCobalt(ctx, rawURL, downloadID, isAudio, func(progress float64, downloaded, total int64) {
					services.Global.SendProgress(downloadID, "downloading", fmt.Sprintf("Downloading... %.0f%%", progress), &progress, nil)
					services.Global.UpdatePendingJob(downloadID, progress, "downloading")
				}, services.CobaltDownloadOpts{})
				if cobaltErr != nil {
					handleDownloadError(w, downloadID, outputExt, cobaltErr)
					return
				}
				downloadedPath = cobaltResult.FilePath
				downloadedExt = cobaltResult.Ext
			} else {
				downloadedPath = result.Path
				downloadedExt = result.Ext
			}
			p := float64(100)
			services.Global.SendProgress(downloadID, "downloading", "Download complete", &p, nil)
		}
	} else {
		result, err := services.DownloadViaYtdlp(ctx, rawURL, downloadID, services.DownloadOpts{
			IsAudio:     isAudio,
			AudioFormat: audioFormat,
			Quality:     quality,
			Container:   container,
			TempDir:     config.TempDirs["download"],
			ProcessInfo: processInfo,
			Playlist:    downloadPlaylist,
			UseProxy:    isYouTube,
			OnProgress: func(progress float64, speed, eta string) {
				services.Global.SendProgress(downloadID, "downloading", fmt.Sprintf("Downloading... %.0f%%", progress), &progress, map[string]interface{}{"speed": speed, "eta": eta})
				services.Global.UpdatePendingJob(downloadID, progress, "downloading")
			},
		})
		if err != nil {
			handleDownloadError(w, downloadID, outputExt, err)
			return
		}
		downloadedPath = result.Path
		downloadedExt = result.Ext
	}

	if downloadedPath == "" {
		handleDownloadError(w, downloadID, outputExt, fmt.Errorf("Downloaded file not found"))
		return
	}
	if _, err := os.Stat(downloadedPath); os.IsNotExist(err) {
		handleDownloadError(w, downloadID, outputExt, fmt.Errorf("Downloaded file not found"))
		return
	}

	isTwitter := strings.Contains(rawURL, "twitter.com") || strings.Contains(rawURL, "x.com")
	isGif := false
	if isTwitter && !isAudio && twitterGifs {
		isGif = services.ProbeForGif(downloadedPath)
	}

	actualOutputExt := outputExt
	actualFinalFile := finalFile
	if isGif {
		actualOutputExt = "gif"
		actualFinalFile = filepath.Join(config.TempDirs["download"], fmt.Sprintf("%s-final.gif", downloadID))
	}

	msg := "Processing video..."
	if isGif {
		msg = "Converting to GIF..."
	}
	p := float64(100)
	services.Global.SendProgress(downloadID, "processing", msg, &p, nil)

	processed, err := services.ProcessVideo(downloadedPath, actualFinalFile, services.ProcessVideoOpts{
		IsAudio:      isAudio,
		IsGif:        isGif,
		AudioFormat:  audioFormat,
		AudioBitrate: audioBitrate,
		Container:    container,
		JobID:        downloadID,
	})
	if err != nil {
		handleDownloadError(w, downloadID, outputExt, err)
		return
	}

	if !processed.Skipped {
		os.Remove(downloadedPath)
	}

	streamPath := actualFinalFile
	if processed.Skipped {
		streamPath = processed.Path
	}

	if _, err := os.Stat(streamPath); os.IsNotExist(err) {
		handleDownloadError(w, downloadID, outputExt, fmt.Errorf("Processing failed - output file not created"))
		return
	}

	if processInfo.IsCancelled() {
		handleDownloadError(w, downloadID, outputExt, fmt.Errorf("Download cancelled"))
		return
	}

	_ = downloadedExt
	services.StreamFile(w, r, streamPath, orDefault(filename, "download"), actualOutputExt,
		services.GetMimeType(actualOutputExt, isAudio, isGif),
		downloadID, rawURL, "download", nil)
}

func handleDownloadError(w http.ResponseWriter, downloadID, outputExt string, err error) {
	log.Printf("[%s] Error: %s", downloadID, err)
	alerts.DownloadFailed(downloadID, "", err)
	services.Global.SendProgressSimple(downloadID, "error", util.ToUserError(err.Error()))
	services.Global.ReleaseJob(downloadID)

	entries, _ := os.ReadDir(config.TempDirs["download"])
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), downloadID) {
			os.Remove(filepath.Join(config.TempDirs["download"], e.Name()))
		}
	}

	if flusher, ok := w.(http.Flusher); ok {
		_ = flusher
	}
	respondJSON(w, 500, map[string]string{"error": util.ToUserError(err.Error())})
}

func tryGalleryDlMetadata(ctx context.Context, rawURL string) (map[string]interface{}, error) {
	if !util.GalleryDlAvailable {
		return nil, fmt.Errorf("gallery-dl not available")
	}

	args := []string{"--dump-json", "--range", "1-10"}
	if _, err := os.Stat(util.CookiesFile); err == nil {
		args = append([]string{"--cookies", util.CookiesFile}, args...)
	}
	args = append(args, rawURL)

	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "gallery-dl", args...)
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("gallery-dl failed")
	}

	stdout := string(out)
	var imageCount int
	title := "Image"
	var images []map[string]interface{}

	var data []json.RawMessage
	if json.Unmarshal([]byte(stdout), &data) == nil {
		for _, raw := range data {
			var arr []json.RawMessage
			if json.Unmarshal(raw, &arr) == nil && len(arr) >= 2 {
				var typeNum float64
				json.Unmarshal(arr[0], &typeNum)
				if typeNum < 0 {
					continue
				}
				var urlStr string
				if json.Unmarshal(arr[1], &urlStr) == nil && strings.HasPrefix(urlStr, "http") {
					imageCount++
					img := map[string]interface{}{"url": urlStr, "filename": fmt.Sprintf("image_%d", imageCount), "extension": "jpg"}
					if len(arr) >= 3 {
						var meta map[string]interface{}
						if json.Unmarshal(arr[2], &meta) == nil {
							if fn, ok := meta["filename"].(string); ok {
								img["filename"] = fn
							}
							if ext, ok := meta["extension"].(string); ok {
								img["extension"] = ext
							}
							if title == "Image" {
								for _, key := range []string{"subcategory", "category", "gallery"} {
									if v, ok := meta[key].(string); ok && v != "" {
										title = v
										break
									}
								}
							}
						}
					}
					images = append(images, img)
				}
			}
		}
	}

	if imageCount == 0 {
		for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
			var item map[string]interface{}
			if json.Unmarshal([]byte(line), &item) == nil {
				imageCount++
				if fn, ok := item["filename"].(string); ok {
					ext, _ := item["extension"].(string)
					if ext == "" {
						ext = "jpg"
					}
					images = append(images, map[string]interface{}{"filename": fn, "extension": ext, "url": item["url"]})
				}
				if title == "Image" {
					for _, key := range []string{"subcategory", "category", "gallery"} {
						if v, ok := item[key].(string); ok && v != "" {
							title = v
							break
						}
					}
				}
			}
		}
	}

	if imageCount == 0 {
		return nil, fmt.Errorf("No images found")
	}

	hostname := ""
	if parsed, err := parseHostname(rawURL); err == nil {
		hostname = parsed
	}

	cap := 10
	if len(images) < cap {
		cap = len(images)
	}

	return map[string]interface{}{
		"title":      title,
		"imageCount": imageCount,
		"images":     images[:cap],
		"site":       hostname,
		"isGallery":  true,
	}, nil
}

func parseHostname(rawURL string) (string, error) {
	if !strings.HasPrefix(rawURL, "http") {
		rawURL = "https://" + rawURL
	}
	parts := strings.Split(rawURL, "/")
	if len(parts) >= 3 {
		host := parts[2]
		host = strings.TrimPrefix(host, "www.")
		if idx := strings.Index(host, ":"); idx >= 0 {
			host = host[:idx]
		}
		return host, nil
	}
	return "", fmt.Errorf("invalid URL")
}

