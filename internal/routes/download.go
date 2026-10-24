package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/coah80/yoink/internal/alerts"
	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
	"github.com/coah80/yoink/internal/util"
)

var resolveSocialMedia = services.ResolveSocialMedia

const statusClientClosedRequest = 499

func DownloadRoutes(r chi.Router) {
	r.Get("/api/metadata", handleMetadata)
	r.Get("/api/download", handleDownload)
	r.Get("/api/recovered/{id}", handleRecoveredDownload)
	r.Head("/api/recovered/{id}", handleRecoveredDownload)
	r.Get("/api/recovered/{id}/status", handleRecoveredDownloadStatus)
}

func handleMetadata(w http.ResponseWriter, r *http.Request) {
	rawURL := util.NormalizeYouTubeURL(r.URL.Query().Get("url"))
	downloadPlaylist := r.URL.Query().Get("playlist") == "true"

	check := util.ValidateURL(rawURL)
	if !check.Valid {
		respondJSON(w, 400, map[string]string{"error": check.Error})
		return
	}

	// Return cached metadata if available (10-minute TTL)
	if !downloadPlaylist {
		if cached, ok := services.MetadataCache.Get(rawURL); ok {
			respondJSON(w, 200, cached)
			return
		}
	}

	isYouTubeURL := util.IsYouTubeURL(rawURL)
	isClip := isYouTubeURL && strings.Contains(rawURL, "/clip/")
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

	if services.IsInstagramURL(rawURL) {
		meta, err := services.FetchInstagramMetadata(ctx, rawURL)
		if err == nil {
			result := map[string]interface{}{
				"title":      meta.Title,
				"ext":        meta.Ext,
				"uploader":   meta.Author,
				"thumbnail":  meta.Thumbnail,
				"mediaType":  meta.MediaType,
				"isPlaylist": false,
			}
			services.MetadataCache.Set(rawURL, result, 10*time.Minute)
			respondJSON(w, 200, result)
			return
		}
		log.Printf("[Metadata] Instagram extractor failed, falling back to yt-dlp: %s", err)
	}

	if services.IsTikTokMusicURL(rawURL) {
		musicID := services.ExtractTikTokMusicID(rawURL)
		result := map[string]interface{}{
			"title":      "TikTok Sound " + musicID,
			"ext":        "mp3",
			"id":         musicID,
			"duration":   "",
			"isPlaylist": false,
			"isAudio":    true,
		}
		services.MetadataCache.Set(rawURL, result, 10*time.Minute)
		respondJSON(w, 200, result)
		return
	}

	if services.IsTikTokURL(rawURL) {
		meta, err := services.FetchTikTokMetadata(ctx, rawURL)
		if err != nil {
			log.Printf("[Metadata] TikTok tikwm failed, falling back to yt-dlp: %s", err)
		} else {
			result := map[string]interface{}{
				"title":       meta.Title,
				"ext":         "mp4",
				"uploader":    meta.Author,
				"duration":    meta.Duration,
				"thumbnail":   meta.Thumbnail,
				"isPlaylist":  false,
				"isSlideshow": meta.IsSlideshow,
				"imageCount":  meta.ImageCount,
			}
			services.MetadataCache.Set(rawURL, result, 10*time.Minute)
			respondJSON(w, 200, result)
			return
		}
	}

	if services.IsTwitterURL(rawURL) {
		meta, err := services.FetchTwitterMetadata(ctx, rawURL)
		if err == nil {
			result := map[string]interface{}{
				"title":      meta.Title,
				"ext":        meta.Ext,
				"id":         services.ExtractTweetID(rawURL),
				"uploader":   meta.Author,
				"duration":   meta.Duration,
				"thumbnail":  meta.Thumbnail,
				"mediaType":  meta.MediaType,
				"isPlaylist": false,
			}
			services.MetadataCache.Set(rawURL, result, 10*time.Minute)
			respondJSON(w, 200, result)
			return
		}
		log.Printf("[Metadata] Twitter extractor failed, falling back to yt-dlp: %s", err)
	}

	usingCookies := isYouTubeURL && util.HasCookiesFile()
	resolved, handled, resolveErr := resolveSocialMedia(ctx, rawURL, 0)
	if resolveErr != nil {
		respondJSON(w, 500, map[string]string{"error": util.ToUserError(resolveErr.Error())})
		return
	}
	if handled {
		respondJSON(w, 200, map[string]interface{}{
			"title":        resolved.Title,
			"ext":          resolved.Ext,
			"id":           resolved.ID,
			"uploader":     resolved.Uploader,
			"duration":     fmt.Sprint(resolved.Duration),
			"thumbnail":    resolved.Thumbnail,
			"isPlaylist":   false,
			"usingCookies": false,
		})
		return
	}

	ytdlpURL := util.NormalizeYtdlpURL(rawURL)
	buildYtdlpMetadataArgs := func() []string {
		args := append([]string{}, util.YtdlpArgsForURL(ytdlpURL)...)
		if isYouTubeURL {
			args = append(args, util.GetProxyArgs()...)
		}
		args = append(args, "--remote-components", "ejs:github")
		if !downloadPlaylist && !util.IsBilibiliURL(ytdlpURL) {
			args = append(args, "-t", "sleep")
		}
		if !downloadPlaylist {
			return append(args, "--no-playlist", "--dump-single-json", ytdlpURL)
		}
		return append(args, "--yes-playlist", "--flat-playlist",
			"--print", "%(playlist_title)s", "--print", "%(playlist_count)s", "--print", "%(title)s",
			ytdlpURL,
		)
	}

	runYtdlpMetadata := func() (string, string, bool, error) {
		cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(cmdCtx, util.YtdlpBinary(), buildYtdlpMetadataArgs()...)
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		stopCancellation := services.ConfigureYtdlpCancellation(cmd, 5*time.Second, stdout, stderr)
		defer stopCancellation()

		if err := cmd.Start(); err != nil {
			return "", "yt-dlp not found. Please install yt-dlp.", false, err
		}

		var outBuf, errBuf strings.Builder
		var streamWG sync.WaitGroup
		streamWG.Add(2)
		go func() {
			defer streamWG.Done()
			_, _ = io.Copy(&outBuf, stdout)
		}()
		go func() {
			defer streamWG.Done()
			_, _ = io.Copy(&errBuf, stderr)
		}()

		streamWG.Wait()
		err := cmd.Wait()
		return outBuf.String(), errBuf.String(), cmdCtx.Err() != nil, err
	}

	tries := 1
	if isYouTubeURL && util.HasProxy() {
		tries = 3
	}
	var outText, errOutput string
	var timedOut bool
	var err error
	for attempt := 1; attempt <= tries; attempt++ {
		if attempt > 1 {
			log.Printf("[Metadata] yt-dlp retry %d/%d with a new proxy", attempt, tries)
		}
		outText, errOutput, timedOut, err = runYtdlpMetadata()
		if ctx.Err() != nil {
			respondJSON(w, 504, map[string]string{"error": "Metadata fetch cancelled"})
			return
		}
		if timedOut {
			// a hung relay is the main reason to rotate, so only give up on the last try
			if attempt < tries {
				continue
			}
			respondJSON(w, 504, map[string]string{"error": "Metadata fetch timed out (30s)"})
			return
		}
		if err == nil {
			break
		}
		if !util.ShouldRetryWithNewProxy(errOutput) {
			break
		}
		if util.NeedsCookiesRetry(errOutput) && util.RefreshCookies("YouTube bot detection during metadata fetch") {
			outText, errOutput, timedOut, err = runYtdlpMetadata()
			if timedOut {
				respondJSON(w, 504, map[string]string{"error": "Metadata fetch timed out (30s)"})
				return
			}
			if err == nil {
				break
			}
		}
	}
	if err != nil {
		if util.NeedsCookiesRetry(errOutput) && isYouTubeURL && !usingCookies {
			alerts.CookieIssue("YouTube bot detection - cookies.txt may be stale or missing")
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

	lines := strings.Split(strings.TrimSpace(outText), "\n")
	if downloadPlaylist {
		playlistTitle := "Playlist"
		if len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
			playlistTitle = lines[0]
		}
		videoCount := 0
		if len(lines) > 1 {
			fmt.Sscanf(lines[1], "%d", &videoCount)
		}
		videoTitles := []string{}
		if len(lines) > 2 {
			for _, t := range lines[2:] {
				if t = strings.TrimSpace(t); t != "" {
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
		return
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(outText)), &metadata); err != nil {
		get := func(i int) string {
			if i < len(lines) {
				return lines[i]
			}
			return ""
		}
		result := map[string]interface{}{
			"title":        orDefault(get(0), "download"),
			"ext":          orDefault(get(1), "mp4"),
			"id":           get(2),
			"uploader":     get(3),
			"duration":     get(4),
			"thumbnail":    get(5),
			"isPlaylist":   false,
			"usingCookies": usingCookies,
		}
		services.MetadataCache.Set(rawURL, result, 10*time.Minute)
		respondJSON(w, 200, result)
		return
	}
	getString := func(key, fallback string) string {
		value, ok := metadata[key].(string)
		if !ok || value == "" || value == "NA" {
			return fallback
		}
		return value
	}
	duration := ""
	if value, ok := metadata["duration"]; ok && value != nil {
		duration = fmt.Sprint(value)
	}
	result := map[string]interface{}{
		"title":        getString("title", "download"),
		"ext":          getString("ext", "mp4"),
		"id":           getString("id", ""),
		"uploader":     getString("uploader", ""),
		"duration":     duration,
		"thumbnail":    getString("thumbnail", ""),
		"isPlaylist":   false,
		"usingCookies": usingCookies,
	}
	services.MetadataCache.Set(rawURL, result, 10*time.Minute)
	respondJSON(w, 200, result)
}

func handleDownload(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rawURL := util.NormalizeYouTubeURL(q.Get("url"))
	format := orDefault(q.Get("format"), "video")
	filename := q.Get("filename")
	quality := orDefault(q.Get("quality"), "1080p")
	container := orDefault(q.Get("container"), "mp4")
	audioFormat := orDefault(q.Get("audioFormat"), "mp3")
	audioBitrate := orDefault(q.Get("audioBitrate"), "320")
	progressID := q.Get("progressId")
	clientID := effectiveClientID(r, q.Get("clientId"))
	twitterGifs := q.Get("twitterGifs") != "false"
	downloadPlaylist := q.Get("playlist") == "true"
	recovery := q.Get("recovery") == "true"
	stemMode := q.Get("stems")
	if stemMode == "off" {
		stemMode = ""
	}
	if stemMode != "" && !config.Contains(services.AllowedStemModes, stemMode) {
		respondJSON(w, 400, map[string]string{"error": "Invalid stems option. Allowed: vocals, instrumental, both"})
		return
	}
	preferences := services.DownloadPreferences{Codec: q.Get("codec"), H265: q.Get("h265") == "true", FilenameStyle: q.Get("filenameStyle"), Uploader: q.Get("uploader")}
	if q.Has("metadata") {
		enabled := q.Get("metadata") != "false"
		preferences.Metadata = &enabled
	}
	if err := preferences.Validate(); err != nil {
		respondJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	filenamePreferences := preferences
	filenamePreferences.Codec = preferences.EffectiveCodec(container)
	filename = services.DownloadFilename(filename, quality, filenamePreferences)

	downloadID := progressID
	if downloadID == "" {
		downloadID = uuid.New().String()
	}

	check := util.ValidateURL(rawURL)
	if !check.Valid {
		respondJSON(w, 400, map[string]string{"error": check.Error})
		return
	}
	if stemMode != "" && (format == "audio" || services.IsTikTokMusicURL(rawURL)) {
		release, err := services.ReserveStemJob()
		if err != nil {
			services.Global.SendProgressSimple(downloadID, "error", util.ToUserError(err.Error()))
			respondJSON(w, 503, map[string]string{"error": util.ToUserError(err.Error())})
			return
		}
		defer release()
	}
	if pending := services.Global.GetPendingJob(downloadID); pending != nil && pending.Ready && util.NormalizeYouTubeURL(pending.URL) == rawURL {
		if info, err := os.Stat(pending.OutputPath); err == nil && info.Size() > 0 {
			streamReadyPendingDownload(w, r, pending)
			return
		}
		services.Global.RemovePendingJob(downloadID)
	}
	if services.Global.GetProcess(downloadID) != nil {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "Download already in progress"})
		return
	}

	if !services.Global.TryReserveClientJob(downloadID, clientID, config.MaxJobsPerClient) {
		services.Global.SendProgressSimple(downloadID, "error", fmt.Sprintf("too many active jobs, max %d at once per person", config.MaxJobsPerClient))
		respondJSON(w, 429, map[string]string{"error": fmt.Sprintf("too many active jobs, max %d at once per person", config.MaxJobsPerClient)})
		return
	}

	jobCheck := services.Global.WaitForJobSlot(r.Context(), "download")
	if !jobCheck.OK {
		services.Global.UnlinkJobFromClient(downloadID)
		services.Global.SendProgressSimple(downloadID, "error", jobCheck.Reason)
		respondJSON(w, 503, map[string]string{"error": jobCheck.Reason})
		return
	}

	isAudio := format == "audio"
	if services.IsTikTokMusicURL(rawURL) {
		isAudio = true
	}
	outputExt := container
	if isAudio {
		outputExt = audioFormat
	}
	finalFile := filepath.Join(config.TempDirs["download"], fmt.Sprintf("%s-final.%s", downloadID, outputExt))

	ctx, cancel := newDownloadWorkContext(r.Context())
	defer cancel()

	processInfo := &services.ProcessInfo{
		TempFile:   finalFile,
		JobType:    "download",
		CancelFunc: cancel,
	}
	if !services.Global.TrySetProcess(downloadID, processInfo) {
		cancel()
		services.Global.DecrementJob("download")
		respondJSON(w, http.StatusConflict, map[string]string{"error": "Download already in progress"})
		return
	}
	if clientID != "" {
		services.Global.RegisterClient(clientID)
		services.Global.LinkJobToClient(downloadID, clientID)
	}

	pending := &services.PendingJob{
		DownloadPreferences: preferences,
		Type:                "download",
		URL:                 rawURL,
		ClientID:            clientID,
		Status:              "starting",
		Filename:            filename,
		Format:              format,
		Quality:             quality,
		Container:           container,
		AudioFormat:         audioFormat,
		AudioBitrate:        audioBitrate,
		TwitterGifs:         twitterGifs,
		Playlist:            downloadPlaylist,
		Stems:               stemMode,
	}
	if recovery {
		if existing := services.Global.GetPendingJob(downloadID); existing != nil {
			pending.CreatedAt = existing.CreatedAt
			pending.Progress = existing.Progress
		}
		pending.Status = "resuming"
	}
	services.Global.RegisterPendingJob(downloadID, pending)

	jobs := services.Global.GetJobsByType()
	jobsJSON, _ := json.Marshal(jobs)
	log.Printf("[Queue] Download started. Active: %s", jobsJSON)
	services.Global.SendProgressSimple(downloadID, "starting", "Initializing download...")

	isYouTube := strings.Contains(rawURL, "youtube.com") || strings.Contains(rawURL, "youtu.be")

	if format == "photo" && isYouTube {
		videoID := util.ExtractYouTubeVideoID(rawURL)
		if videoID == "" {
			services.Global.SendProgressSimple(downloadID, "error", "Could not extract YouTube video ID")
			services.Global.ReleaseJob(downloadID)
			respondJSON(w, 400, map[string]string{"error": "Could not extract YouTube video ID"})
			return
		}

		services.Global.SendProgressWithPercent(downloadID, "downloading", "Fetching thumbnail...", 0)

		thumbURLs := []string{
			fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", videoID),
			fmt.Sprintf("https://i.ytimg.com/vi/%s/sddefault.jpg", videoID),
			fmt.Sprintf("https://i.ytimg.com/vi/%s/hqdefault.jpg", videoID),
		}

		thumbPath := filepath.Join(config.TempDirs["download"], fmt.Sprintf("%s-thumb.jpg", downloadID))
		var thumbErr error
		for _, thumbURL := range thumbURLs {
			resp, err := http.Get(thumbURL)
			if err != nil {
				thumbErr = err
				continue
			}
			if resp.StatusCode != 200 {
				resp.Body.Close()
				continue
			}
			out, err := os.Create(thumbPath)
			if err != nil {
				resp.Body.Close()
				thumbErr = err
				break
			}
			_, err = io.Copy(out, resp.Body)
			resp.Body.Close()
			out.Close()
			if err != nil {
				thumbErr = err
				break
			}
			thumbErr = nil
			break
		}

		if thumbErr != nil || func() bool { _, err := os.Stat(thumbPath); return os.IsNotExist(err) }() {
			errMsg := "Could not download YouTube thumbnail"
			if thumbErr != nil {
				errMsg = thumbErr.Error()
			}
			services.Global.SendProgressSimple(downloadID, "error", errMsg)
			services.Global.ReleaseJob(downloadID)
			respondJSON(w, 500, map[string]string{"error": errMsg})
			return
		}

		p := float64(100)
		services.Global.SendProgress(downloadID, "downloading", "Thumbnail downloaded", &p, nil)
		services.StreamFile(w, r, thumbPath, orDefault(filename, "thumbnail"), "jpg", "image/jpeg", downloadID, rawURL, "download", nil)
		return
	}
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
			result, err := services.HandleClipDownload(ctx, clipData, downloadID, config.TempDirs["download"], quality, preferences.EffectiveCodec(container), container, func(progress float64, speed, eta string) {
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
				DownloadPreferences: preferences,
				IsAudio:             isAudio,
				AudioFormat:         audioFormat,
				Quality:             quality,
				Container:           container,
				TempDir:             config.TempDirs["download"],
				ProcessInfo:         processInfo,
				Playlist:            false,
				UseProxy:            isYouTube,
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
	} else if services.IsTikTokMusicURL(rawURL) {
		services.Global.SendProgressWithPercent(downloadID, "downloading", "Fetching TikTok audio...", 0)
		result, err := services.DownloadTikTokMusic(ctx, rawURL, downloadID, config.TempDirs["download"], func(progress float64, downloaded, total int64) {
			services.Global.SendProgress(downloadID, "downloading", fmt.Sprintf("Downloading audio... %.0f%%", progress), &progress, nil)
			services.Global.UpdatePendingJob(downloadID, progress, "downloading")
		})
		if err != nil {
			handleDownloadError(w, downloadID, outputExt, err)
			return
		}
		downloadedPath = result.Path
		downloadedExt = result.Ext
		isAudio = true
		p := float64(100)
		services.Global.SendProgress(downloadID, "downloading", "Download complete", &p, nil)
	} else if services.IsTikTokURL(rawURL) {
		label := "Downloading TikTok video..."
		if isAudio {
			label = "Downloading TikTok audio..."
		}
		services.Global.SendProgressWithPercent(downloadID, "downloading", label, 0)
		result, err := services.DownloadTikTokVideo(ctx, rawURL, downloadID, config.TempDirs["download"], isAudio, func(progress float64, downloaded, total int64) {
			services.Global.SendProgress(downloadID, "downloading", fmt.Sprintf("Downloading... %.0f%%", progress), &progress, nil)
			services.Global.UpdatePendingJob(downloadID, progress, "downloading")
		})
		if err != nil {
			handleDownloadError(w, downloadID, outputExt, err)
			return
		}
		downloadedPath = result.Path
		downloadedExt = result.Ext
		p := float64(100)
		services.Global.SendProgress(downloadID, "downloading", "Download complete", &p, nil)
	} else if services.IsTwitterURL(rawURL) {
		services.Global.SendProgressWithPercent(downloadID, "downloading", "Downloading Twitter media...", 0)
		result, err := services.DownloadTwitterMedia(ctx, rawURL, downloadID, config.TempDirs["download"], isAudio, twitterGifs, func(progress float64, downloaded, total int64) {
			services.Global.SendProgress(downloadID, "downloading", fmt.Sprintf("Downloading... %.0f%%", progress), &progress, nil)
			services.Global.UpdatePendingJob(downloadID, progress, "downloading")
		})
		if err != nil {
			log.Printf("[%s] Twitter fxtwitter failed, falling back to yt-dlp: %s", downloadID, err)
			services.Global.SendProgressWithPercent(downloadID, "downloading", "Retrying via yt-dlp...", 0)
			ytResult, ytErr := services.DownloadViaYtdlp(ctx, rawURL, downloadID, services.DownloadOpts{
				IsAudio:             isAudio,
				AudioFormat:         audioFormat,
				Quality:             quality,
				DownloadPreferences: preferences,
				Container:           container,
				TempDir:             config.TempDirs["download"],
				ProcessInfo:         processInfo,
				Playlist:            false,
				UseProxy:            false,
				OnProgress: func(progress float64, speed, eta string) {
					services.Global.SendProgress(downloadID, "downloading", fmt.Sprintf("Downloading... %.0f%%", progress), &progress, map[string]interface{}{"speed": speed, "eta": eta})
					services.Global.UpdatePendingJob(downloadID, progress, "downloading")
				},
			})
			if ytErr != nil {
				handleDownloadError(w, downloadID, outputExt, ytErr)
				return
			}
			downloadedPath = ytResult.Path
			downloadedExt = ytResult.Ext
		} else {
			downloadedPath = result.Path
			downloadedExt = result.Ext
		}
		p := float64(100)
		services.Global.SendProgress(downloadID, "downloading", "Download complete", &p, nil)
	} else if services.IsInstagramURL(rawURL) {
		services.Global.SendProgressWithPercent(downloadID, "downloading", "Downloading Instagram media...", 0)
		result, err := services.DownloadInstagramMedia(ctx, rawURL, downloadID, config.TempDirs["download"], isAudio, func(progress float64, downloaded, total int64) {
			services.Global.SendProgress(downloadID, "downloading", fmt.Sprintf("Downloading... %.0f%%", progress), &progress, nil)
			services.Global.UpdatePendingJob(downloadID, progress, "downloading")
		})
		if err != nil {
			log.Printf("[%s] Instagram Cobalt failed, falling back to yt-dlp: %s", downloadID, err)
			services.Global.SendProgressWithPercent(downloadID, "downloading", "Retrying via yt-dlp...", 0)
			ytResult, ytErr := services.DownloadViaYtdlp(ctx, rawURL, downloadID, services.DownloadOpts{
				IsAudio:             isAudio,
				AudioFormat:         audioFormat,
				Quality:             quality,
				DownloadPreferences: preferences,
				Container:           container,
				TempDir:             config.TempDirs["download"],
				ProcessInfo:         processInfo,
				Playlist:            false,
				UseProxy:            false,
				OnProgress: func(progress float64, speed, eta string) {
					services.Global.SendProgress(downloadID, "downloading", fmt.Sprintf("Downloading... %.0f%%", progress), &progress, map[string]interface{}{"speed": speed, "eta": eta})
					services.Global.UpdatePendingJob(downloadID, progress, "downloading")
				},
			})
			if ytErr != nil {
				handleDownloadError(w, downloadID, outputExt, ytErr)
				return
			}
			downloadedPath = ytResult.Path
			downloadedExt = ytResult.Ext
		} else {
			downloadedPath = result.Path
			downloadedExt = result.Ext
		}
		p := float64(100)
		services.Global.SendProgress(downloadID, "downloading", "Download complete", &p, nil)
	} else {
		result, err := services.DownloadViaYtdlp(ctx, rawURL, downloadID, services.DownloadOpts{
			DownloadPreferences: preferences,
			IsAudio:             isAudio,
			AudioFormat:         audioFormat,
			Quality:             quality,
			Container:           container,
			TempDir:             config.TempDirs["download"],
			ProcessInfo:         processInfo,
			Playlist:            downloadPlaylist,
			UseProxy:            isYouTube,
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

	if downloadedExt == "zip" {
		services.StreamFile(w, r, downloadedPath, orDefault(filename, "download"), "zip", "application/zip", downloadID, rawURL, "download", nil)
		return
	}

	if isAudio && stemMode != "" {
		streamStems(w, r, ctx, processInfo, downloadID, downloadedPath, rawURL, filename, stemMode, recovery, services.ProcessVideoOpts{
			DownloadPreferences: preferences,
			IsAudio:             true,
			AudioFormat:         audioFormat,
			AudioBitrate:        audioBitrate,
			JobID:               downloadID,
		})
		return
	}

	isGif := shouldOutputGIF(rawURL, downloadedExt, isAudio, twitterGifs, downloadedPath)

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
	services.Global.UpdatePendingJob(downloadID, 100, "processing")
	services.Global.SendProgress(downloadID, "processing", msg, &p, nil)

	processed, err := services.ProcessVideo(downloadedPath, actualFinalFile, services.ProcessVideoOpts{
		DownloadPreferences: preferences,
		IsAudio:             isAudio,
		IsGif:               isGif,
		AudioFormat:         audioFormat,
		AudioBitrate:        audioBitrate,
		Container:           container,
		JobID:               downloadID,
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

	if recovery {
		if err := services.Global.MarkPendingJobReady(downloadID, streamPath, actualOutputExt); err != nil {
			handleDownloadError(w, downloadID, outputExt, err)
			return
		}
		downloadURL := "/api/recovered/" + downloadID
		services.Global.SendProgress(downloadID, "ready", "Recovered download is ready...", &p, map[string]interface{}{
			"downloadUrl": downloadURL,
			"filename":    orDefault(filename, "download"),
			"ext":         actualOutputExt,
		})
		services.Global.ReleaseJobKeepingPending(downloadID)
		respondJSON(w, 200, map[string]interface{}{"ready": true, "downloadUrl": downloadURL})
		return
	}

	_ = downloadedExt
	services.StreamFile(w, r, streamPath, orDefault(filename, "download"), actualOutputExt,
		services.GetMimeType(actualOutputExt, isAudio, isGif),
		downloadID, rawURL, "download", nil)
}

func newDownloadWorkContext(requestContext context.Context) (context.Context, context.CancelFunc) {
	return context.WithCancel(context.WithoutCancel(requestContext))
}

func shouldOutputGIF(rawURL, downloadedExt string, isAudio, twitterGifs bool, downloadedPath string) bool {
	if isAudio {
		return false
	}
	if strings.EqualFold(downloadedExt, "gif") {
		return true
	}
	isTwitter := strings.Contains(rawURL, "twitter.com") || strings.Contains(rawURL, "x.com")
	return isTwitter && twitterGifs && services.ProbeForGif(downloadedPath)
}

func handleDownloadError(w http.ResponseWriter, downloadID, outputExt string, err error) {
	if services.Global.IsShuttingDown() {
		log.Printf("[%s] Server restart interrupted the request; preserving its journal and partial media", downloadID)
		services.Global.ReleaseJobKeepingPending(downloadID)
		return
	}
	if strings.EqualFold(strings.TrimSpace(err.Error()), "Download cancelled") {
		log.Printf("[%s] Download cancelled", downloadID)
		util.CleanupJobFiles(downloadID)
		services.Global.ReleaseJob(downloadID)
		respondJSON(w, statusClientClosedRequest, map[string]string{"error": "Download cancelled"})
		return
	}
	pending := services.Global.GetPendingJob(downloadID)
	rawURL := ""
	stage := "download"
	if pending != nil {
		rawURL = pending.URL
		if pending.Status != "" {
			stage = pending.Status
		}
	}
	log.Printf("[%s] Error: %s", downloadID, err)
	if logErr := services.RecordDownloadFailure(downloadID, rawURL, stage, err); logErr != nil {
		log.Printf("[%s] Failed to write structured failure log: %v", downloadID, logErr)
	}
	alerts.DownloadFailed(downloadID, "", err)
	if util.NeedsCookiesRetry(err.Error()) {
		util.TriggerCookieRefresh("YouTube bot detection during download")
	}
	services.Global.SendProgressSimple(downloadID, "error", util.ToUserError(err.Error()))

	entries, _ := os.ReadDir(config.TempDirs["download"])
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), downloadID) {
			os.Remove(filepath.Join(config.TempDirs["download"], e.Name()))
		}
	}
	services.Global.ReleaseJob(downloadID)

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
