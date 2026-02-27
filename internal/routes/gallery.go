package routes

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
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

func GalleryRoutes(r chi.Router) {
	r.Get("/api/gallery/status", handleGalleryStatus)
	r.Get("/api/gallery/metadata", handleGalleryMetadata)
	r.Get("/api/gallery/download", handleGalleryDownload)
	r.Get("/api/gallery/slideshow", handleSlideshow)
}

func handleGalleryStatus(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, 200, map[string]bool{"available": util.GalleryDlAvailable})
}

func handleGalleryMetadata(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")

	if !util.GalleryDlAvailable {
		respondJSON(w, 503, map[string]string{"error": "gallery-dl not installed on server"})
		return
	}

	if validation := util.ValidateURL(rawURL); !validation.Valid {
		respondJSON(w, 400, map[string]string{"error": validation.Error})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gallery-dl", "--dump-json", "--range", "1-10", rawURL)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	stdout, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			respondJSON(w, 500, map[string]string{"error": "gallery-dl metadata timeout (30s)"})
			return
		}
		log.Printf("[gallery-dl] metadata error: %s\n", stderrBuf.String())
		respondJSON(w, 500, map[string]interface{}{
			"error": "Could not fetch gallery info",
		})
		return
	}

	imageCount := 0
	title := "Image"
	var images []map[string]string
	var dirMeta map[string]interface{}

	var arrayData []json.RawMessage
	if err := json.Unmarshal(stdout, &arrayData); err == nil {
		for _, raw := range arrayData {
			var entry []json.RawMessage
			if json.Unmarshal(raw, &entry) != nil || len(entry) < 2 {
				continue
			}
			var idx float64
			if json.Unmarshal(entry[0], &idx) != nil || idx < 0 {
				continue
			}
			var urlStr string
			if json.Unmarshal(entry[1], &urlStr) == nil && strings.HasPrefix(urlStr, "http") {
				meta := make(map[string]interface{})
				if len(entry) >= 3 {
					json.Unmarshal(entry[2], &meta)
				}
				imageCount++
				images = append(images, map[string]string{
					"filename":  strOrDefault(meta, "filename", fmt.Sprintf("image_%d", imageCount)),
					"extension": strOrDefault(meta, "extension", "jpg"),
					"url":       urlStr,
				})
				if title == "Image" {
					title = firstNonEmpty(meta, "subcategory", "category", "gallery")
				}
			} else {
				var meta map[string]interface{}
				if json.Unmarshal(entry[1], &meta) == nil {
					if dirMeta == nil {
						dirMeta = meta
					}
					if title == "Image" {
						title = firstNonEmpty(meta, "subcategory", "category", "gallery")
					}
				}
			}
		}
	}

	if imageCount == 0 {
		lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var item map[string]interface{}
			if json.Unmarshal([]byte(line), &item) != nil {
				continue
			}
			imageCount++
			img := map[string]string{
				"extension": strOrDefault(item, "extension", "jpg"),
			}
			if fn, ok := item["filename"].(string); ok {
				img["filename"] = fn
			}
			if u, ok := item["url"].(string); ok {
				img["url"] = u
			}
			images = append(images, img)
			if title == "Image" {
				title = firstNonEmpty(item, "subcategory", "category", "gallery")
			}
		}
	}

	if imageCount == 0 {
		respondJSON(w, 500, map[string]string{"error": "No images found in this link"})
		return
	}

	parsed, _ := url.Parse(rawURL)
	hostname := strings.TrimPrefix(parsed.Hostname(), "www.")

	result := map[string]interface{}{
		"title":      title,
		"imageCount": imageCount,
		"images":     limitSlice(images, 10),
		"site":       hostname,
		"isGallery":  true,
	}

	if dirMeta != nil {
		if cat, _ := dirMeta["category"].(string); cat == "tiktok" {
			if pt, _ := dirMeta["post_type"].(string); pt == "image" {
				result["isTikTokCarousel"] = true
				if music, ok := dirMeta["music"].(map[string]interface{}); ok {
					playURL, _ := music["playUrl"].(string)
					playURL2, _ := music["play_url"].(string)
					result["hasAudio"] = playURL != "" || playURL2 != ""
					if mt, ok := music["title"].(string); ok {
						result["musicTitle"] = mt
					}
					if md, ok := music["duration"].(float64); ok {
						result["musicDuration"] = md
					}
				}
			}
		}
	}

	respondJSON(w, 200, result)
}

func handleGalleryDownload(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	progressID := r.URL.Query().Get("progressId")
	clientID := r.URL.Query().Get("clientId")
	filename := r.URL.Query().Get("filename")

	if !util.GalleryDlAvailable {
		respondJSON(w, 503, map[string]string{"error": "gallery-dl not installed on server"})
		return
	}

	if validation := util.ValidateURL(rawURL); !validation.Valid {
		respondJSON(w, 400, map[string]string{"error": validation.Error})
		return
	}

	if clientID != "" {
		if services.Global.GetClientJobCount(clientID) >= config.MaxJobsPerClient {
			respondJSON(w, 429, map[string]string{
				"error": fmt.Sprintf("Too many active jobs. Maximum %d concurrent jobs per user.", config.MaxJobsPerClient),
			})
			return
		}
	}

	downloadID := progressID
	if downloadID == "" {
		downloadID = uuid.New().String()
	}

	jobCheck := services.Global.CanStartJob("download")
	if !jobCheck.OK {
		services.Global.SendProgressSimple(downloadID, "error", jobCheck.Reason)
		respondJSON(w, 503, map[string]string{"error": jobCheck.Reason})
		return
	}

	galleryDir := filepath.Join(config.TempDirs["gallery"], "gallery-"+downloadID)
	os.MkdirAll(galleryDir, 0755)

	if clientID != "" {
		services.Global.RegisterClient(clientID)
		services.Global.LinkJobToClient(downloadID, clientID)
	}

	processInfo := &services.ProcessInfo{TempDir: galleryDir, JobType: "download"}
	services.Global.SetProcess(downloadID, processInfo)

	log.Println("[Queue] Gallery download started.")
	services.Global.SendProgressSimple(downloadID, "starting", "Starting gallery download...")

	cleanup := func() {
		services.Global.ReleaseJob(downloadID)
		log.Println("[Queue] Gallery finished.")
		go func() {
			time.Sleep(2 * time.Second)
			util.CleanupJobFiles(downloadID)
		}()
	}

	err := runGalleryDl(rawURL, galleryDir, downloadID, processInfo, r)
	if err != nil {
		galleryError(w, downloadID, processInfo, err, cleanup)
		return
	}

	allFiles := collectDownloadedFiles(galleryDir)
	if len(allFiles) == 0 {
		galleryError(w, downloadID, processInfo, fmt.Errorf("No images were downloaded"), cleanup)
		return
	}

	if len(allFiles) == 1 {
		sendGallerySingleFile(w, allFiles[0], filename, downloadID, cleanup)
	} else {
		sendGalleryZipFile(w, allFiles, filename, rawURL, downloadID, cleanup)
	}
}

func handleSlideshow(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	progressID := r.URL.Query().Get("progressId")
	clientID := r.URL.Query().Get("clientId")
	filename := r.URL.Query().Get("filename")

	if !util.GalleryDlAvailable {
		respondJSON(w, 503, map[string]string{"error": "gallery-dl not installed on server"})
		return
	}

	if validation := util.ValidateURL(rawURL); !validation.Valid {
		respondJSON(w, 400, map[string]string{"error": validation.Error})
		return
	}

	if clientID != "" {
		if services.Global.GetClientJobCount(clientID) >= config.MaxJobsPerClient {
			respondJSON(w, 429, map[string]string{
				"error": fmt.Sprintf("Too many active jobs. Maximum %d concurrent jobs per user.", config.MaxJobsPerClient),
			})
			return
		}
	}

	downloadID := progressID
	if downloadID == "" {
		downloadID = uuid.New().String()
	}

	jobCheck := services.Global.CanStartJob("download")
	if !jobCheck.OK {
		services.Global.SendProgressSimple(downloadID, "error", jobCheck.Reason)
		respondJSON(w, 503, map[string]string{"error": jobCheck.Reason})
		return
	}

	galleryDir := filepath.Join(config.TempDirs["gallery"], "gallery-"+downloadID)
	os.MkdirAll(galleryDir, 0755)

	if clientID != "" {
		services.Global.RegisterClient(clientID)
		services.Global.LinkJobToClient(downloadID, clientID)
	}

	processInfo := &services.ProcessInfo{TempDir: galleryDir, JobType: "download"}
	services.Global.SetProcess(downloadID, processInfo)

	log.Println("[Queue] Slideshow download started.")
	services.Global.SendProgressSimple(downloadID, "starting", "Starting slideshow download...")

	cleanup := func() {
		services.Global.ReleaseJob(downloadID)
		log.Println("[Queue] Slideshow finished.")
		go func() {
			time.Sleep(2 * time.Second)
			util.CleanupJobFiles(downloadID)
		}()
	}

	err := runGalleryDl(rawURL, galleryDir, downloadID, processInfo, r)
	if err != nil {
		galleryError(w, downloadID, processInfo, err, cleanup)
		return
	}

	allFiles := collectDownloadedFiles(galleryDir)
	if len(allFiles) == 0 {
		galleryError(w, downloadID, processInfo, fmt.Errorf("No files were downloaded"), cleanup)
		return
	}

	imageExts := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true, ".bmp": true, ".tiff": true}
	audioExts := map[string]bool{".mp3": true, ".m4a": true, ".wav": true, ".ogg": true, ".aac": true, ".opus": true}

	var imageFiles, audioFiles []string
	for _, f := range allFiles {
		ext := strings.ToLower(filepath.Ext(f))
		if imageExts[ext] {
			imageFiles = append(imageFiles, f)
		} else if audioExts[ext] {
			audioFiles = append(audioFiles, f)
		}
	}

	if len(imageFiles) == 0 {
		galleryError(w, downloadID, processInfo, fmt.Errorf("No images found in download"), cleanup)
		return
	}

	if len(audioFiles) == 0 {
		log.Printf("[%s] No audio found, falling back to gallery download\n", downloadID)
		if len(allFiles) == 1 {
			sendGallerySingleFile(w, allFiles[0], filename, downloadID, cleanup)
		} else {
			sendGalleryZipFile(w, allFiles, filename, rawURL, downloadID, cleanup)
		}
		return
	}

	audioFile := audioFiles[0]
	services.Global.SendProgressWithPercent(downloadID, "processing", "Analyzing audio...", 30)
	audioDuration := getAudioDuration(audioFile)
	log.Printf("[%s] Audio duration: %.1fs, Images: %d\n", downloadID, audioDuration, len(imageFiles))

	dims := getImageDimensions(imageFiles[0])
	isVertical := dims.height >= dims.width
	targetW, targetH := 1920, 1080
	if isVertical {
		targetW, targetH = 1080, 1920
	}

	outputFile := filepath.Join(galleryDir, downloadID+"-slideshow.mp4")
	services.Global.SendProgressWithPercent(downloadID, "processing", "Creating slideshow video...", 50)

	crossfadeDur := 0.5

	if len(imageFiles) == 1 {
		ffArgs := []string{
			"-loop", "1", "-t", fmt.Sprintf("%.3f", audioDuration),
			"-i", imageFiles[0],
			"-i", audioFile,
			"-filter_complex",
			fmt.Sprintf("[0]scale=w='if(gt(iw,ih),%d,-2)':h='if(gt(iw,ih),-2,%d)',pad=%d:%d:(ow-iw)/2:(oh-ih)/2:black,setsar=1[v]",
				targetW, targetH, targetW, targetH),
			"-map", "[v]", "-map", "1:a",
			"-shortest", "-r", "30", "-pix_fmt", "yuv420p",
			"-c:v", "libx264", "-preset", "fast", "-crf", "23",
			"-c:a", "aac", "-b:a", "192k",
			outputFile,
		}
		err = runSlideshowFfmpeg(ffArgs, downloadID, processInfo)
	} else {
		secPerImage := audioDuration / float64(len(imageFiles))
		imgDuration := secPerImage
		if imgDuration < crossfadeDur+0.1 {
			imgDuration = crossfadeDur + 0.1
		}

		var ffArgs []string
		for _, img := range imageFiles {
			ffArgs = append(ffArgs, "-loop", "1", "-t", fmt.Sprintf("%.3f", imgDuration), "-i", img)
		}
		ffArgs = append(ffArgs, "-i", audioFile)

		var filterParts []string
		for i := range imageFiles {
			filterParts = append(filterParts,
				fmt.Sprintf("[%d]scale=w='if(gt(iw,ih),%d,-2)':h='if(gt(iw,ih),-2,%d)',pad=%d:%d:(ow-iw)/2:(oh-ih)/2:black,setsar=1[s%d]",
					i, targetW, targetH, targetW, targetH, i))
		}

		prevLabel := "s0"
		for i := 1; i < len(imageFiles); i++ {
			offset := imgDuration*float64(i) - crossfadeDur*float64(i)
			if offset < 0 {
				offset = 0
			}
			outLabel := fmt.Sprintf("x%d", i)
			if i == len(imageFiles)-1 {
				outLabel = "v"
			}
			filterParts = append(filterParts,
				fmt.Sprintf("[%s][s%d]xfade=transition=fade:duration=%.1f:offset=%.3f[%s]",
					prevLabel, i, crossfadeDur, offset, outLabel))
			prevLabel = outLabel
		}

		ffArgs = append(ffArgs, "-filter_complex", strings.Join(filterParts, ";"))
		ffArgs = append(ffArgs, "-map", "[v]", "-map", fmt.Sprintf("%d:a", len(imageFiles)))
		ffArgs = append(ffArgs, "-shortest", "-r", "30", "-pix_fmt", "yuv420p")
		ffArgs = append(ffArgs, "-c:v", "libx264", "-preset", "fast", "-crf", "23")
		ffArgs = append(ffArgs, "-c:a", "aac", "-b:a", "192k")
		ffArgs = append(ffArgs, outputFile)
		err = runSlideshowFfmpeg(ffArgs, downloadID, processInfo)
	}

	if err != nil {
		galleryError(w, downloadID, processInfo, err, cleanup)
		return
	}

	if _, statErr := os.Stat(outputFile); statErr != nil {
		galleryError(w, downloadID, processInfo, fmt.Errorf("Slideshow creation failed - output file not found"), cleanup)
		return
	}

	sendGallerySingleFile(w, outputFile, filename, downloadID, cleanup)
}

func runGalleryDl(rawURL, galleryDir, downloadID string, processInfo *services.ProcessInfo, r *http.Request) error {
	args := []string{
		"-d", galleryDir,
		"--filename", "{num:03d}_{filename}.{extension}",
		"--write-metadata",
		rawURL,
	}

	if cookieArgs := util.GetCookiesArgs(); len(cookieArgs) > 0 {
		args = append([]string{"--cookies", cookieArgs[1]}, args...)
	}

	log.Printf("[%s] gallery-dl starting\n", downloadID)

	cmd := exec.Command("gallery-dl", args...)
	processInfo.SetCmd(cmd)

	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start gallery-dl: %w", err)
	}

	go func() {
		buf := make([]byte, 4096)
		downloadedCount := 0
		lastUpdate := time.Now()
		for {
			n, err := stdoutPipe.Read(buf)
			if n > 0 {
				msg := string(buf[:n])
				if strings.Contains(msg, "/") || strings.Contains(msg, ".jpg") || strings.Contains(msg, ".png") || strings.Contains(msg, ".gif") || strings.Contains(msg, ".webp") {
					downloadedCount++
					if time.Since(lastUpdate) > 500*time.Millisecond {
						lastUpdate = time.Now()
						services.Global.SendProgress(downloadID, "downloading",
							fmt.Sprintf("Downloaded %d images...", downloadedCount),
							nil, map[string]interface{}{"downloadedCount": downloadedCount})
					}
				}
			}
			if err != nil {
				break
			}
		}
	}()

	var stderrBuf strings.Builder
	go func() {
		io.Copy(&stderrBuf, stderrPipe)
	}()

	done := make(chan struct{})
	go func() {
		select {
		case <-r.Context().Done():
			processInfo.SetCancelled(true)
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		case <-done:
		}
	}()

	err := cmd.Wait()
	close(done)

	if processInfo.IsCancelled() {
		return fmt.Errorf("Download cancelled")
	}
	if err != nil {
		errMsg := strings.TrimSpace(stderrBuf.String())
		log.Printf("[%s] gallery-dl exited with error: %s\n", downloadID, truncStr(errMsg, 200))
		return fmt.Errorf("gallery-dl failed: %s", truncStr(errMsg, 200))
	}
	return nil
}

func collectDownloadedFiles(dir string) []string {
	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && !strings.HasSuffix(info.Name(), ".json") {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func sendGallerySingleFile(w http.ResponseWriter, filePath, filename, downloadID string, cleanup func()) {
	ext := strings.ToLower(filepath.Ext(filePath))
	mimeTypes := map[string]string{
		".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
		".gif": "image/gif", ".webp": "image/webp", ".mp4": "video/mp4", ".webm": "video/webm",
	}
	mimeType := mimeTypes[ext]
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	stat, err := os.Stat(filePath)
	if err != nil {
		services.Global.SendProgressSimple(downloadID, "error", "Failed to send file")
		cleanup()
		return
	}

	safeName := util.SanitizeFilename(filename)
	if safeName == "" {
		safeName = util.SanitizeFilename(strings.TrimSuffix(filepath.Base(filePath), ext))
	}
	safeName += ext

	services.Global.SendProgressSimple(downloadID, "sending", "Sending file...")

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, safeName, url.PathEscape(safeName)))

	f, err := os.Open(filePath)
	if err != nil {
		services.Global.SendProgressSimple(downloadID, "error", "Failed to send file")
		cleanup()
		return
	}
	defer f.Close()

	io.Copy(w, f)
	services.Global.SendProgressSimple(downloadID, "complete", "Download complete!")
	cleanup()
}

func sendGalleryZipFile(w http.ResponseWriter, allFiles []string, filename, rawURL, downloadID string, cleanup func()) {
	services.Global.SendProgressWithPercent(downloadID, "zipping",
		fmt.Sprintf("Creating zip with %d images...", len(allFiles)), 90)

	zipPath := filepath.Join(config.TempDirs["gallery"], downloadID+".zip")
	parsed, _ := url.Parse(rawURL)
	hostname := strings.TrimPrefix(parsed.Hostname(), "www.")
	safeZipName := util.SanitizeFilename(filename)
	if safeZipName == "" {
		safeZipName = util.SanitizeFilename(hostname)
	}
	if safeZipName == "" {
		safeZipName = "gallery"
	}

	zipFile, err := os.Create(zipPath)
	if err != nil {
		services.Global.SendProgressSimple(downloadID, "error", "Failed to create zip")
		cleanup()
		return
	}

	zw := zip.NewWriter(zipFile)
	for _, fp := range allFiles {
		f, err := os.Open(fp)
		if err != nil {
			continue
		}
		w, err := zw.Create(filepath.Base(fp))
		if err != nil {
			f.Close()
			continue
		}
		io.Copy(w, f)
		f.Close()
	}
	zw.Close()
	zipFile.Close()

	services.Global.SendProgressSimple(downloadID, "sending", "Sending zip file...")

	stat, err := os.Stat(zipPath)
	if err != nil {
		services.Global.SendProgressSimple(downloadID, "error", "Failed to send zip")
		cleanup()
		return
	}

	zipFilename := safeZipName + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, zipFilename, url.PathEscape(zipFilename)))

	f, err := os.Open(zipPath)
	if err != nil {
		services.Global.SendProgressSimple(downloadID, "error", "Failed to send zip")
		cleanup()
		return
	}
	defer f.Close()

	io.Copy(w, f)
	services.Global.SendProgressSimple(downloadID, "complete",
		fmt.Sprintf("Downloaded %d images!", len(allFiles)))
	cleanup()
}

func runSlideshowFfmpeg(args []string, downloadID string, processInfo *services.ProcessInfo) error {
	log.Printf("[%s] ffmpeg starting slideshow render\n", downloadID)
	cmd := exec.Command("ffmpeg", append([]string{"-y"}, args...)...)
	processInfo.SetCmd(cmd)

	stderrPipe, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}

	var stderrBuf strings.Builder
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				stderrBuf.Write(buf[:n])
				if stderrBuf.Len() > 10000 {
					s := stderrBuf.String()
					stderrBuf.Reset()
					stderrBuf.WriteString(s[len(s)-5000:])
				}
			}
			if err != nil {
				break
			}
		}
	}()

	timer := time.AfterFunc(5*time.Minute, func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
	})
	defer timer.Stop()

	err := cmd.Wait()
	if processInfo.IsCancelled() {
		return fmt.Errorf("Download cancelled")
	}
	if err != nil {
		errStr := stderrBuf.String()
		if len(errStr) > 500 {
			errStr = errStr[len(errStr)-500:]
		}
		log.Printf("[%s] ffmpeg error: %s\n", downloadID, errStr)
		return fmt.Errorf("Failed to create slideshow video")
	}
	return nil
}

func galleryError(w http.ResponseWriter, downloadID string, processInfo *services.ProcessInfo, err error, cleanup func()) {
	log.Printf("[%s] Gallery error: %s\n", downloadID, err.Error())
	if !processInfo.IsCancelled() {
		alerts.GalleryFailed(downloadID, "", err)
		services.Global.SendProgressSimple(downloadID, "error", err.Error())
	}
	cleanup()
	respondJSON(w, 500, map[string]string{"error": err.Error()})
}

type imageDims struct {
	width, height int
}

func getAudioDuration(filePath string) float64 {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", filePath)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	var dur float64
	fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &dur)
	return dur
}

func getImageDimensions(filePath string) imageDims {
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height", "-of", "csv=p=0:s=x", filePath)
	out, err := cmd.Output()
	if err != nil {
		return imageDims{1080, 1920}
	}
	var w, h int
	fmt.Sscanf(strings.TrimSpace(string(out)), "%dx%d", &w, &h)
	if w == 0 {
		w = 1080
	}
	if h == 0 {
		h = 1920
	}
	return imageDims{w, h}
}

func strOrDefault(m map[string]interface{}, key, def string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return def
}

func firstNonEmpty(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return "Image"
}

func limitSlice(s []map[string]string, n int) []map[string]string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func truncStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

