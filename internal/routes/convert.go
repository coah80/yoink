package routes

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
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

func ConvertRoutes(r chi.Router) {
	r.Post("/api/convert", handleConvert)
	r.Post("/api/compress", handleCompress)
	r.Post("/api/upload/init", handleUploadInit)
	r.Post("/api/upload/chunk/{uploadId}/{chunkIndex}", handleUploadChunk)
	r.Post("/api/upload/complete/{uploadId}", handleUploadComplete)
	r.Post("/api/convert-chunked", handleConvertChunked)
	r.Post("/api/compress-chunked", handleCompressChunked)
	r.Post("/api/fetch-url", handleFetchURL)
	r.Get("/api/job/{jobId}/status", handleJobStatus)
	r.Get("/api/job/{jobId}/download", handleJobDownload)
}

var allowedUploadExts = map[string]bool{
	".mp4": true, ".webm": true, ".mkv": true, ".mov": true, ".avi": true, ".flv": true, ".wmv": true,
	".mp3": true, ".m4a": true, ".wav": true, ".flac": true, ".ogg": true, ".opus": true, ".aac": true, ".wma": true,
	".ts": true, ".m4v": true, ".3gp": true, ".mpg": true, ".mpeg": true,
}

func saveUploadedFile(r *http.Request, w http.ResponseWriter, fieldName string) (string, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, config.FileSizeLimit)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return "", "", fmt.Errorf("Failed to parse upload: file may be too large")
	}

	file, header, err := r.FormFile(fieldName)
	if err != nil {
		return "", "", fmt.Errorf("No file uploaded")
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" || !allowedUploadExts[ext] {
		return "", "", fmt.Errorf("Unsupported file type. Please upload a media file.")
	}
	tmpPath := filepath.Join(config.TempDirs["upload"], uuid.New().String()+ext)
	dst, err := os.Create(tmpPath)
	if err != nil {
		return "", "", fmt.Errorf("Failed to save file")
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		os.Remove(tmpPath)
		return "", "", fmt.Errorf("Failed to save file")
	}

	return tmpPath, header.Filename, nil
}

func resolveFilePath(input string) string {
	if input == "" {
		return ""
	}

	ref := services.Global.GetFileRef(input)
	if ref != nil {
		services.Global.DeleteFileRef(input)
		return ref.FilePath
	}

	resolved, err := filepath.Abs(input)
	if err != nil {
		return ""
	}
	uploadDir := config.TempDirs["upload"] + string(filepath.Separator)
	if !strings.HasPrefix(resolved, uploadDir) && resolved != config.TempDirs["upload"] {
		return ""
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return ""
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	return resolved
}

func handleUploadInit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FileName    string      `json:"fileName"`
		FileSize    json.Number `json:"fileSize"`
		TotalChunks int         `json:"totalChunks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, 400, map[string]string{"error": "Invalid request body"})
		return
	}

	if body.FileName == "" || body.FileSize == "" || body.TotalChunks == 0 {
		respondJSON(w, 400, map[string]string{"error": "Missing fileName, fileSize, or totalChunks"})
		return
	}

	fileSize, err := body.FileSize.Int64()
	if err != nil || fileSize <= 0 {
		respondJSON(w, 400, map[string]string{"error": "fileSize must be a positive number"})
		return
	}
	if fileSize > config.FileSizeLimit {
		respondJSON(w, 400, map[string]string{
			"error": fmt.Sprintf("File too large. Maximum size is %dGB", config.FileSizeLimit/(1024*1024*1024)),
		})
		return
	}
	if body.TotalChunks > 200 {
		respondJSON(w, 400, map[string]string{"error": "Too many chunks (max 200)"})
		return
	}

	uploadID := uuid.New().String()
	services.Global.SetChunkedUpload(uploadID, &services.ChunkedUpload{
		FileName:       body.FileName,
		FileSize:       fileSize,
		TotalChunks:    body.TotalChunks,
		ReceivedChunks: make(map[int]bool),
		LastActivity:   time.Now(),
	})

	log.Printf("[Chunk] Initialized upload %s: (%.1fMB, %d chunks)\n",
		uploadID[:8], float64(fileSize)/(1024*1024), body.TotalChunks)
	respondJSON(w, 200, map[string]string{"uploadId": uploadID})
}

func handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	uploadID := chi.URLParam(r, "uploadId")
	chunkIndexStr := chi.URLParam(r, "chunkIndex")
	index, err := strconv.Atoi(chunkIndexStr)
	if err != nil {
		respondJSON(w, 400, map[string]string{"error": "Invalid chunk index"})
		return
	}

	uploadData := services.Global.GetChunkedUpload(uploadID)
	if uploadData == nil {
		respondJSON(w, 404, map[string]string{"error": "Upload not found or expired"})
		return
	}

	if index < 0 || index >= uploadData.TotalChunks {
		respondJSON(w, 400, map[string]string{"error": "Invalid chunk index"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, int64(config.ChunkSize+1024*1024))
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		respondJSON(w, 400, map[string]string{"error": "Failed to parse chunk"})
		return
	}

	chunkFile, _, err := r.FormFile("chunk")
	if err != nil {
		respondJSON(w, 400, map[string]string{"error": "No chunk data"})
		return
	}
	defer chunkFile.Close()

	chunkPath := filepath.Join(config.TempDirs["upload"],
		fmt.Sprintf("chunk-%s-%04d", uploadID, index))

	dst, err := os.Create(chunkPath)
	if err != nil {
		respondJSON(w, 500, map[string]string{"error": "Failed to save chunk. Disk may be full or permissions issue."})
		return
	}
	if _, err := io.Copy(dst, chunkFile); err != nil {
		dst.Close()
		os.Remove(chunkPath)
		respondJSON(w, 500, map[string]string{"error": "Failed to save chunk."})
		return
	}
	dst.Close()

	received, total := uploadData.MarkChunkReceived(index)
	log.Printf("[Chunk] Upload %s: chunk %d/%d\n", uploadID[:8], index+1, total)

	respondJSON(w, 200, map[string]interface{}{
		"received": received,
		"total":    total,
		"complete": received == total,
	})
}

func handleUploadComplete(w http.ResponseWriter, r *http.Request) {
	uploadID := chi.URLParam(r, "uploadId")

	uploadData := services.Global.GetChunkedUpload(uploadID)
	if uploadData == nil {
		respondJSON(w, 404, map[string]string{"error": "Upload not found or expired"})
		return
	}

	if complete, received, total := uploadData.IsComplete(); !complete {
		respondJSON(w, 400, map[string]string{
			"error": fmt.Sprintf("Missing chunks: received %d/%d", received, total),
		})
		return
	}

	assembledPath := filepath.Join(config.TempDirs["upload"],
		"assembled-"+uploadID+"-"+util.SanitizeFilename(uploadData.FileName))

	outFile, err := os.Create(assembledPath)
	if err != nil {
		respondJSON(w, 500, map[string]string{"error": "Failed to assemble file"})
		return
	}

	for i := 0; i < uploadData.TotalChunks; i++ {
		chunkPath := filepath.Join(config.TempDirs["upload"],
			fmt.Sprintf("chunk-%s-%04d", uploadID, i))
		chunk, err := os.Open(chunkPath)
		if err != nil {
			outFile.Close()
			os.Remove(assembledPath)
			respondJSON(w, 500, map[string]string{"error": "Failed to assemble file"})
			return
		}
		io.Copy(outFile, chunk)
		chunk.Close()
		os.Remove(chunkPath)
	}
	outFile.Close()

	services.Global.DeleteChunkedUpload(uploadID)

	fileToken := uuid.New().String()
	services.Global.SetFileRef(fileToken, &services.FileRef{
		FilePath:  assembledPath,
		FileName:  uploadData.FileName,
		CreatedAt: time.Now(),
	})

	log.Printf("[Chunk] Upload %s assembled (ref: %s)\n", uploadID[:8], fileToken[:8])
	respondJSON(w, 200, map[string]interface{}{
		"success":  true,
		"filePath": fileToken,
		"fileName": uploadData.FileName,
	})
}

func handleJobStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	job := services.Global.GetAsyncJob(jobID)
	if job == nil {
		respondJSON(w, 404, map[string]string{"error": "Job not found or expired"})
		return
	}

	status, progress, message, errMsg, textContent := job.GetStatus()
	resp := map[string]interface{}{
		"status":   status,
		"progress": progress,
		"message":  message,
		"error":    nilIfEmpty(errMsg),
	}
	if textContent != "" {
		resp["textContent"] = textContent
	}
	respondJSON(w, 200, resp)
}

func handleJobDownload(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	job := services.Global.GetAsyncJob(jobID)
	if job == nil {
		respondJSON(w, 404, map[string]string{"error": "Job not found or expired"})
		return
	}

	outputPath, outputFilename, mimeType, status := job.GetDownloadInfo()
	if status != "complete" {
		respondJSON(w, 400, map[string]string{"error": "Job not complete yet"})
		return
	}
	if outputPath == "" {
		respondJSON(w, 404, map[string]string{"error": "Output file not found"})
		return
	}
	stat, err := os.Stat(outputPath)
	if err != nil {
		respondJSON(w, 404, map[string]string{"error": "Output file not found"})
		return
	}
	if mimeType == "" {
		mimeType = "video/mp4"
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		outputFilename, url.PathEscape(outputFilename)))

	f, err := os.Open(outputPath)
	if err != nil {
		respondJSON(w, 500, map[string]string{"error": "Failed to read file"})
		return
	}
	defer f.Close()

	io.Copy(w, f)

	go func() {
		time.Sleep(5 * time.Second)
		os.Remove(outputPath)
		services.Global.DeleteAsyncJob(jobID)
	}()
}

func handleFetchURL(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.URL) == "" {
		respondJSON(w, 400, map[string]string{"error": "Missing or invalid URL"})
		return
	}

	trimmedURL := strings.TrimSpace(body.URL)
	if validation := util.ValidateURL(trimmedURL); !validation.Valid {
		respondJSON(w, 400, map[string]string{"error": validation.Error})
		return
	}

	fetchCheck := services.Global.CanStartJob("fetchUrl")
	if !fetchCheck.OK {
		respondJSON(w, 503, map[string]string{"error": fetchCheck.Reason})
		return
	}

	id := "fetch-" + uuid.New().String()
	isYouTube := strings.Contains(trimmedURL, "youtube.com") || strings.Contains(trimmedURL, "youtu.be")
	log.Printf("[%s] Fetching URL (yt-dlp)\n", id)

	ytdlpFetch := func() (string, error) {
		args := []string{}
		if isYouTube {
			args = append(args, util.GetProxyArgs()...)
		}
		args = append(args,
			"--no-playlist",
			"-f", "bv*+ba/b",
			"-o", filepath.Join(config.TempDirs["upload"], id+"-%(title)s.%(ext)s"),
			"--print", "after_move:filepath",
			"--no-warnings",
			trimmedURL,
		)
		cmd := exec.Command("yt-dlp", args...)
		out, err := cmd.Output()
		if err != nil {
			var stderrMsg string
			if ee, ok := err.(*exec.ExitError); ok {
				lines := strings.Split(strings.TrimSpace(string(ee.Stderr)), "\n")
				if len(lines) > 0 {
					stderrMsg = lines[len(lines)-1]
				}
			}
			if stderrMsg == "" {
				stderrMsg = "yt-dlp failed"
			}
			return "", fmt.Errorf("%s", stderrMsg)
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		outputPath := lines[len(lines)-1]
		if _, err := os.Stat(outputPath); err != nil {
			return "", fmt.Errorf("yt-dlp did not produce a file")
		}
		return outputPath, nil
	}

	var filePath string
	var fetchErr error

	if isYouTube {
		filePath, fetchErr = ytdlpFetch()
		if fetchErr != nil {
			log.Printf("[%s] yt-dlp failed, falling back to Cobalt: %s\n", id, fetchErr.Error())
			result, cobaltErr := services.DownloadViaCobalt(r.Context(), trimmedURL, id, false, nil, services.CobaltDownloadOpts{OutputDir: config.TempDirs["upload"]})
			if cobaltErr != nil {
				services.Global.DecrementJob("fetchUrl")
				respondJSON(w, 400, map[string]string{"error": fetchErr.Error()})
				return
			}
			filePath = result.FilePath
		}
	} else {
		filePath, fetchErr = ytdlpFetch()
		if fetchErr != nil {
			services.Global.DecrementJob("fetchUrl")
			respondJSON(w, 400, map[string]string{"error": fetchErr.Error()})
			return
		}
	}

	stat, err := os.Stat(filePath)
	if err != nil {
		services.Global.DecrementJob("fetchUrl")
		respondJSON(w, 500, map[string]string{"error": "Failed to stat downloaded file"})
		return
	}

	if stat.Size() > config.FileSizeLimit {
		os.Remove(filePath)
		services.Global.DecrementJob("fetchUrl")
		respondJSON(w, 400, map[string]string{
			"error": fmt.Sprintf("Downloaded file too large (%.1fGB). Maximum is %dGB.",
				float64(stat.Size())/(1024*1024*1024), config.FileSizeLimit/(1024*1024*1024)),
		})
		return
	}

	duration, width, height := probeVideoInfo(filePath)

	fileName := filepath.Base(filePath)
	log.Printf("[%s] Fetched: %s (%.1fMB)\n", id, fileName, float64(stat.Size())/(1024*1024))

	fileToken := uuid.New().String()
	services.Global.SetFileRef(fileToken, &services.FileRef{
		FilePath:  filePath,
		FileName:  fileName,
		CreatedAt: time.Now(),
	})

	services.Global.DecrementJob("fetchUrl")
	respondJSON(w, 200, map[string]interface{}{
		"filePath": fileToken,
		"fileName": fileName,
		"fileSize": stat.Size(),
		"duration": duration,
		"width":    width,
		"height":   height,
	})
}

func handleConvert(w http.ResponseWriter, r *http.Request) {
	filePath, originalName, err := saveUploadedFile(r, w, "file")
	if err != nil {
		respondJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	format := formValueOr(r, "format", "mp4")
	clientID := r.FormValue("clientId")
	quality := formValueOr(r, "quality", "medium")
	reencode := formValueOr(r, "reencode", "auto")
	startTime := r.FormValue("startTime")
	endTime := r.FormValue("endTime")
	rawBitrate := formValueOr(r, "audioBitrate", "192")
	cropRatio := r.FormValue("cropRatio")

	audioBitrate := rawBitrate
	if !config.Contains(config.AllowedAudioBitrates, audioBitrate) {
		audioBitrate = "192"
	}

	if clientID != "" {
		if services.Global.GetClientJobCount(clientID) >= config.MaxJobsPerClient {
			os.Remove(filePath)
			respondJSON(w, 429, map[string]string{
				"error": fmt.Sprintf("Too many active jobs. Maximum %d concurrent jobs per user.", config.MaxJobsPerClient),
			})
			return
		}
	}

	if cropRatio != "" && !config.Contains(config.AllowedCropRatios, cropRatio) {
		os.Remove(filePath)
		respondJSON(w, 400, map[string]string{
			"error": fmt.Sprintf("Invalid crop ratio. Allowed: %s", strings.Join(config.AllowedCropRatios, ", ")),
		})
		return
	}

	convertCheck := services.Global.CanStartJob("convert")
	if !convertCheck.OK {
		os.Remove(filePath)
		respondJSON(w, 503, map[string]string{"error": convertCheck.Reason})
		return
	}

	convertID := uuid.New().String()
	outputPath := filepath.Join(config.TempDirs["convert"], convertID+"-converted."+format)

	if clientID != "" {
		services.Global.RegisterClient(clientID)
		services.Global.LinkJobToClient(convertID, clientID)
	}

	log.Printf("[%s] Converting to %s\n", convertID, format)

	isAudioFormat := isAudioFmt(format)

	validStartTime := util.ValidateTimeParam(startTime)
	validEndTime := util.ValidateTimeParam(endTime)

	if startTime != "" && validStartTime == "" {
		os.Remove(filePath)
		services.Global.DecrementJob("convert")
		services.Global.UnlinkJobFromClient(convertID)
		respondJSON(w, 400, map[string]string{"error": "Invalid startTime format. Use seconds or HH:MM:SS"})
		return
	}
	if endTime != "" && validEndTime == "" {
		os.Remove(filePath)
		services.Global.DecrementJob("convert")
		services.Global.UnlinkJobFromClient(convertID)
		respondJSON(w, 400, map[string]string{"error": "Invalid endTime format. Use seconds or HH:MM:SS"})
		return
	}
	if validStartTime != "" && validEndTime != "" {
		s, _ := strconv.ParseFloat(validStartTime, 64)
		e, _ := strconv.ParseFloat(validEndTime, 64)
		if e <= s {
			os.Remove(filePath)
			services.Global.DecrementJob("convert")
			services.Global.UnlinkJobFromClient(convertID)
			respondJSON(w, 400, map[string]string{"error": "endTime must be greater than startTime"})
			return
		}
	}

	ffmpegArgs := []string{"-y"}
	if validStartTime != "" {
		ffmpegArgs = append(ffmpegArgs, "-ss", validStartTime)
	}
	if validEndTime != "" {
		ffmpegArgs = append(ffmpegArgs, "-to", validEndTime)
	}
	ffmpegArgs = append(ffmpegArgs, "-i", filePath, "-threads", "0")

	if isAudioFormat {
		ffmpegArgs = append(ffmpegArgs, audioCodecArgs(format, audioBitrate)...)
		ffmpegArgs = append(ffmpegArgs, "-vn")
	} else {
		probeCodec := probeVideoCodec(filePath)

		codecCompat := map[string][]string{
			"mp4": {"h264", "avc", "hevc", "h265"}, "webm": {"vp8", "vp9", "av1"},
			"mkv": {"*"}, "mov": {"h264", "hevc", "prores"},
		}
		compat := codecCompat[format]
		isCompatible := sliceContains(compat, "*") || sliceContainsAny(compat, probeCodec)
		needsReencode := reencode == "always" || (reencode == "auto" && !isCompatible) || cropRatio != ""

		var cropFilter string
		if cropRatio != "" {
			cropFilter, _ = buildCropFilter(filePath, cropRatio, nil, convertID)
		}

		if needsReencode {
			crfValues := map[string]int{"high": 18, "medium": 23, "low": 28}
			crf := crfValues[quality]
			if crf == 0 {
				crf = 23
			}
			if cropFilter != "" {
				ffmpegArgs = append(ffmpegArgs, "-vf", cropFilter)
			}
			ffmpegArgs = append(ffmpegArgs, videoCodecArgs(format, crf)...)
		} else {
			ffmpegArgs = append(ffmpegArgs, "-codec", "copy")
		}

		if format == "mp4" || format == "mov" {
			ffmpegArgs = append(ffmpegArgs, "-movflags", "+faststart")
		}
	}

	ffmpegArgs = append(ffmpegArgs, outputPath)

	cmd := exec.Command("ffmpeg", ffmpegArgs...)
	if err := cmd.Run(); err != nil {
		alerts.ConversionFailed(convertID, format, fmt.Errorf("ffmpeg conversion failed: %w", err))
		os.Remove(filePath)
		os.Remove(outputPath)
		services.Global.DecrementJob("convert")
		services.Global.UnlinkJobFromClient(convertID)
		if !isHeaderSent(w) {
			respondJSON(w, 500, map[string]string{"error": "Conversion failed"})
		}
		return
	}

	os.Remove(filePath)

	stat, err := os.Stat(outputPath)
	if err != nil {
		services.Global.DecrementJob("convert")
		services.Global.UnlinkJobFromClient(convertID)
		if !isHeaderSent(w) {
			respondJSON(w, 500, map[string]string{"error": "Output file not found"})
		}
		return
	}
	baseName := strings.TrimSuffix(filepath.Base(originalName), filepath.Ext(originalName))
	outputFilename := util.SanitizeFilename(baseName) + "." + format
	mimeType := getMimeForFormat(format, isAudioFormat)

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		outputFilename, url.PathEscape(outputFilename)))

	f, err := os.Open(outputPath)
	if err != nil {
		services.Global.DecrementJob("convert")
		services.Global.UnlinkJobFromClient(convertID)
		if !isHeaderSent(w) {
			respondJSON(w, 500, map[string]string{"error": "Failed to read output file"})
		}
		return
	}
	defer f.Close()
	io.Copy(w, f)

	log.Printf("[%s] Conversion complete\n", convertID)
	services.Global.DecrementJob("convert")
	services.Global.UnlinkJobFromClient(convertID)
	go func() {
		time.Sleep(2 * time.Second)
		util.CleanupJobFiles(convertID)
	}()
}

func handleCompress(w http.ResponseWriter, r *http.Request) {
	filePath, originalName, err := saveUploadedFile(r, w, "file")
	if err != nil {
		respondJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	targetSize := formValueOr(r, "targetSize", "50")
	durationStr := formValueOr(r, "duration", "0")
	progressID := r.FormValue("progressId")
	clientID := r.FormValue("clientId")
	mode := formValueOr(r, "mode", "size")
	quality := formValueOr(r, "quality", "medium")
	preset := formValueOr(r, "preset", "balanced")
	denoise := formValueOr(r, "denoise", "auto")
	downscale := r.FormValue("downscale")

	shouldDownscale := downscale == "true"

	for _, check := range []struct {
		list []string
		val  string
		name string
	}{
		{config.AllowedModes, mode, "mode"},
		{config.AllowedQualities, quality, "quality"},
		{config.AllowedPresets, preset, "preset"},
		{config.AllowedDenoise, denoise, "denoise"},
	} {
		if !config.Contains(check.list, check.val) {
			os.Remove(filePath)
			respondJSON(w, 400, map[string]string{
				"error": fmt.Sprintf("Invalid %s. Allowed: %s", check.name, strings.Join(check.list, ", ")),
			})
			return
		}
	}

	targetMB, _ := strconv.ParseFloat(targetSize, 64)
	videoDuration, _ := strconv.ParseFloat(durationStr, 64)

	if videoDuration > float64(config.MaxVideoDuration) {
		os.Remove(filePath)
		respondJSON(w, 400, map[string]string{
			"error": fmt.Sprintf("Video too long. Maximum duration is %d hours.", config.MaxVideoDuration/3600),
		})
		return
	}

	if clientID != "" {
		if services.Global.GetClientJobCount(clientID) >= config.MaxJobsPerClient {
			os.Remove(filePath)
			respondJSON(w, 429, map[string]string{
				"error": fmt.Sprintf("Too many active jobs. Maximum %d concurrent jobs per user.", config.MaxJobsPerClient),
			})
			return
		}
	}

	compressCheck := services.Global.CanStartJob("compress")
	if !compressCheck.OK {
		os.Remove(filePath)
		respondJSON(w, 503, map[string]string{"error": compressCheck.Reason})
		return
	}

	compressID := progressID
	if compressID == "" {
		compressID = uuid.New().String()
	}
	outputPath := filepath.Join(config.TempDirs["compress"], compressID+"-compressed.mp4")
	passLogFile := filepath.Join(config.TempDirs["compress"], compressID+"-pass")

	if clientID != "" {
		services.Global.RegisterClient(clientID)
		services.Global.LinkJobToClient(compressID, clientID)
	}

	log.Printf("[%s] Compressing | Mode: %s | Preset: %s\n", compressID, mode, preset)

	processInfo := &services.ProcessInfo{TempFile: outputPath, JobType: "compress"}
	services.Global.SetProcess(compressID, processInfo)

	services.Global.SendProgressWithPercent(compressID, "compressing", "Analyzing video...", 0)

	if !util.ValidateVideoFile(filePath) {
		os.Remove(filePath)
		services.Global.SendProgressSimple(compressID, "error", "File does not contain valid video")
		services.Global.ReleaseJob(compressID)
		respondJSON(w, 500, map[string]string{"error": "File does not contain valid video"})
		return
	}

	probe := probeVideoFull(filePath)
	actualDuration := videoDuration
	if actualDuration <= 0 {
		actualDuration = probe.duration
	}
	sourceFileSizeMB := float64(fileSizeBytes(filePath)) / (1024 * 1024)
	sourceBitrateMbps := (sourceFileSizeMB * 8) / actualDuration

	presetConfig := config.CompressionPresets[preset]
	denoiseFilter := util.GetDenoiseFilter(denoise, probe.height, sourceBitrateMbps, presetConfig.Denoise)
	var downscaleWidth int
	if shouldDownscale {
		downscaleWidth = util.GetDownscaleResolution(probe.width, probe.height)
	}

	if mode == "quality" {
		crf := presetConfig.CRF[quality]
		vfArg := util.BuildVideoFilters(denoiseFilter, downscaleWidth, probe.width)

		log.Printf("[%s] CRF mode: preset=%s, quality=%s, crf=%d\n", compressID, preset, quality, crf)
		services.Global.SendProgressWithPercent(compressID, "compressing", fmt.Sprintf("Encoding (%s)...", preset), 5)

		err := runCrfEncode(filePath, outputPath, crf, presetConfig.FFmpegPreset, vfArg,
			presetConfig.X264Params, processInfo, compressID, actualDuration)
		if err != nil {
			compressError(w, compressID, processInfo, err, filePath, outputPath, passLogFile)
			return
		}
	} else {
		if sourceFileSizeMB <= targetMB {
			services.Global.SendProgressWithPercent(compressID, "compressing", "File already small enough...", 50)
			cmd := exec.Command("ffmpeg", "-y", "-i", filePath, "-c:v", "copy", "-c:a", "copy", "-movflags", "+faststart", outputPath)
			processInfo.SetCmd(cmd)
			if err := cmd.Run(); err != nil {
				compressError(w, compressID, processInfo, fmt.Errorf("Remux failed"), filePath, outputPath, passLogFile)
				return
			}
		} else {
			videoBitrateK := util.CalculateTargetBitrate(targetMB, actualDuration, 96)
			resolution := util.SelectResolution(probe.width, probe.height, videoBitrateK)
			scaleWidth := downscaleWidth
			if scaleWidth == 0 && resolution.NeedsScale {
				scaleWidth = resolution.Width
			}
			vfArg := util.BuildVideoFilters(denoiseFilter, scaleWidth, probe.width)

			log.Printf("[%s] Two-pass: target=%.0fMB, bitrate=%dk, res=%dx%d\n",
				compressID, targetMB, videoBitrateK, resolution.Width, resolution.Height)

			err := runTwoPassEncode(filePath, outputPath, passLogFile, videoBitrateK,
				presetConfig.FFmpegPreset, vfArg, presetConfig.X264Params,
				processInfo, compressID, actualDuration)
			if err != nil {
				compressError(w, compressID, processInfo, err, filePath, outputPath, passLogFile)
				return
			}
		}
	}

	os.Remove(filePath)
	os.Remove(passLogFile + "-0.log")
	os.Remove(passLogFile + "-0.log.mbtree")

	services.Global.SendProgressWithPercent(compressID, "compressing", "Sending file...", 98)

	stat, err := os.Stat(outputPath)
	if err != nil {
		services.Global.ReleaseJob(compressID)
		if !isHeaderSent(w) {
			respondJSON(w, 500, map[string]string{"error": "Output file not found"})
		}
		return
	}
	baseName := strings.TrimSuffix(filepath.Base(originalName), filepath.Ext(originalName))
	outputFilename := util.SanitizeFilename(baseName) + "_compressed.mp4"

	log.Printf("[%s] Complete: %.2fMB\n", compressID, float64(stat.Size())/(1024*1024))

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		outputFilename, url.PathEscape(outputFilename)))

	f, err := os.Open(outputPath)
	if err != nil {
		services.Global.ReleaseJob(compressID)
		if !isHeaderSent(w) {
			respondJSON(w, 500, map[string]string{"error": "Failed to read output file"})
		}
		return
	}
	defer f.Close()
	io.Copy(w, f)

	services.Global.SendProgressWithPercent(compressID, "complete", "Compression complete!", 100)
	services.Global.ReleaseJob(compressID)
	log.Println("[Queue] Compress finished.")
	go func() {
		time.Sleep(2 * time.Second)
		util.CleanupJobFiles(compressID)
	}()
}

func handleConvertChunked(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FilePath     string             `json:"filePath"`
		FileName     string             `json:"fileName"`
		Format       string             `json:"format"`
		ClientID     string             `json:"clientId"`
		Quality      string             `json:"quality"`
		Reencode     string             `json:"reencode"`
		StartTime    string             `json:"startTime"`
		EndTime      string             `json:"endTime"`
		AudioBitrate string             `json:"audioBitrate"`
		CropRatio    string             `json:"cropRatio"`
		CropX        *int               `json:"cropX"`
		CropY        *int               `json:"cropY"`
		CropW        *int               `json:"cropW"`
		CropH        *int               `json:"cropH"`
		Segments     []convertSegment   `json:"segments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, 400, map[string]string{"error": "Invalid request body"})
		return
	}

	validPath := resolveFilePath(body.FilePath)
	if validPath == "" {
		respondJSON(w, 400, map[string]string{"error": "Invalid file path"})
		return
	}
	if _, err := os.Stat(validPath); err != nil {
		respondJSON(w, 400, map[string]string{"error": "File not found. Complete chunked upload first."})
		return
	}

	format := defaultStr(body.Format, "mp4")
	quality := defaultStr(body.Quality, "medium")
	reencode := defaultStr(body.Reencode, "auto")
	audioBitrate := defaultStr(body.AudioBitrate, "192")
	if !config.Contains(config.AllowedAudioBitrates, audioBitrate) {
		audioBitrate = "192"
	}

	if !config.Contains(config.AllowedFormats, format) {
		os.Remove(validPath)
		respondJSON(w, 400, map[string]string{"error": fmt.Sprintf("Invalid format. Allowed: %s", strings.Join(config.AllowedFormats, ", "))})
		return
	}
	if !config.Contains(config.AllowedReencodes, reencode) {
		os.Remove(validPath)
		respondJSON(w, 400, map[string]string{"error": fmt.Sprintf("Invalid reencode option. Allowed: %s", strings.Join(config.AllowedReencodes, ", "))})
		return
	}
	if !config.Contains(config.AllowedQualities, quality) {
		os.Remove(validPath)
		respondJSON(w, 400, map[string]string{"error": fmt.Sprintf("Invalid quality. Allowed: %s", strings.Join(config.AllowedQualities, ", "))})
		return
	}
	if body.CropRatio != "" && !config.Contains(config.AllowedCropRatios, body.CropRatio) {
		os.Remove(validPath)
		respondJSON(w, 400, map[string]string{"error": fmt.Sprintf("Invalid crop ratio. Allowed: %s", strings.Join(config.AllowedCropRatios, ", "))})
		return
	}

	hasRawCrop := body.CropX != nil && body.CropY != nil && body.CropW != nil && body.CropH != nil
	if hasRawCrop {
		cx, cy, cw, ch := *body.CropX, *body.CropY, *body.CropW, *body.CropH
		if cx < 0 || cy < 0 || cw <= 0 || ch <= 0 {
			os.Remove(validPath)
			respondJSON(w, 400, map[string]string{"error": "Invalid crop parameters: values must be positive"})
			return
		}
		if cw%2 != 0 || ch%2 != 0 {
			os.Remove(validPath)
			respondJSON(w, 400, map[string]string{"error": "Invalid crop parameters: width and height must be even"})
			return
		}
	}

	if len(body.Segments) > 0 {
		if len(body.Segments) > config.MaxSegments {
			os.Remove(validPath)
			respondJSON(w, 400, map[string]string{"error": fmt.Sprintf("Too many segments (max %d)", config.MaxSegments)})
			return
		}
		for _, seg := range body.Segments {
			if seg.End <= seg.Start {
				os.Remove(validPath)
				respondJSON(w, 400, map[string]string{"error": "Invalid segment: each must have numeric start < end"})
				return
			}
		}
	}

	jobID := uuid.New().String()
	services.Global.SetAsyncJob(jobID, &services.AsyncJob{
		Status:    "processing",
		Progress:  0,
		Message:   "Starting conversion...",
		CreatedAt: time.Now(),
	})

	respondJSON(w, 200, map[string]string{"jobId": jobID})

	go func() {
		err := handleConvertAsync(validPath, defaultStr(body.FileName, "video.mp4"),
			format, body.ClientID, quality, reencode, body.StartTime, body.EndTime,
			audioBitrate, body.CropRatio, body.CropX, body.CropY, body.CropW, body.CropH,
			body.Segments, jobID)
		if err != nil {
			log.Printf("[AsyncJob] Convert job %s failed: %s\n", jobID, err.Error())
			alerts.ConversionFailed(jobID, format, err)
			job := services.Global.GetAsyncJob(jobID)
			if job != nil {
				job.SetError(err.Error())
			}
		}
	}()
}

func handleCompressChunked(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FilePath  string `json:"filePath"`
		FileName  string `json:"fileName"`
		ClientID  string `json:"clientId"`
		TargetSize string `json:"targetSize"`
		Duration  string `json:"duration"`
		Mode      string `json:"mode"`
		Quality   string `json:"quality"`
		Preset    string `json:"preset"`
		Denoise   string `json:"denoise"`
		Downscale interface{} `json:"downscale"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, 400, map[string]string{"error": "Invalid request body"})
		return
	}

	validPath := resolveFilePath(body.FilePath)
	if validPath == "" {
		respondJSON(w, 400, map[string]string{"error": "Invalid file path"})
		return
	}
	if _, err := os.Stat(validPath); err != nil {
		respondJSON(w, 400, map[string]string{"error": "File not found. Complete chunked upload first."})
		return
	}

	jobID := uuid.New().String()
	services.Global.SetAsyncJob(jobID, &services.AsyncJob{
		Status:    "processing",
		Progress:  0,
		Message:   "Starting compression...",
		CreatedAt: time.Now(),
	})

	respondJSON(w, 200, map[string]string{"jobId": jobID})

	go func() {
		err := handleCompressAsync(validPath, defaultStr(body.FileName, "video.mp4"),
			body.ClientID, defaultStr(body.TargetSize, "50"), defaultStr(body.Duration, "0"),
			defaultStr(body.Mode, "size"), defaultStr(body.Quality, "medium"),
			defaultStr(body.Preset, "balanced"), defaultStr(body.Denoise, "auto"),
			isTruthy(body.Downscale), jobID)
		if err != nil {
			log.Printf("[AsyncJob] Compress job %s failed: %s\n", jobID, err.Error())
			alerts.CompressionFailed(jobID, err)
			job := services.Global.GetAsyncJob(jobID)
			if job != nil {
				job.SetError(err.Error())
			}
		}
	}()
}

type convertSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

func handleConvertAsync(inputPath, originalName, format, clientID, quality, reencode,
	startTime, endTime, audioBitrate, cropRatio string,
	cropX, cropY, cropW, cropH *int,
	segments []convertSegment, jobID string) error {

	job := services.Global.GetAsyncJob(jobID)
	if job == nil {
		return nil
	}

	convertID := jobID
	outputPath := filepath.Join(config.TempDirs["convert"], convertID+"-converted."+format)

	convertCheck := services.Global.CanStartJob("convert")
	if !convertCheck.OK {
		os.Remove(inputPath)
		job.SetError(convertCheck.Reason)
		return nil
	}

	if clientID != "" {
		services.Global.RegisterClient(clientID)
		services.Global.LinkJobToClient(convertID, clientID)
	}

	log.Printf("[%s] Converting to %s (async)\n", convertID, format)

	if cropRatio != "" && !config.Contains(config.AllowedCropRatios, cropRatio) {
		os.Remove(inputPath)
		services.Global.DecrementJob("convert")
		services.Global.UnlinkJobFromClient(convertID)
		job.SetError("Invalid crop ratio")
		return nil
	}
	if startTime != "" && util.ValidateTimeParam(startTime) == "" {
		os.Remove(inputPath)
		services.Global.DecrementJob("convert")
		services.Global.UnlinkJobFromClient(convertID)
		job.SetError("Invalid startTime format")
		return nil
	}
	if endTime != "" && util.ValidateTimeParam(endTime) == "" {
		os.Remove(inputPath)
		services.Global.DecrementJob("convert")
		services.Global.UnlinkJobFromClient(convertID)
		job.SetError("Invalid endTime format")
		return nil
	}

	hasRawCrop := cropX != nil && cropY != nil && cropW != nil && cropH != nil
	var cropOpts interface{}
	if hasRawCrop {
		cropOpts = &rawCropParams{*cropX, *cropY, *cropW, *cropH}
	} else if cropRatio != "" {
		cropOpts = cropRatio
	}
	hasCrop := cropOpts != nil

	hasSegments := len(segments) > 1

	var tempClips []string

	isAudioFormat := isAudioFmt(format)

	job.SetProgressAndMessage(5, "Analyzing file...")

	duration := probeDuration(inputPath)
	if duration <= 0 {
		duration = 60
	}

	var cropFilter string
	if hasCrop {
		switch v := cropOpts.(type) {
		case *rawCropParams:
			cropFilter, _ = buildCropFilter(inputPath, "", v, convertID)
		case string:
			cropFilter, _ = buildCropFilter(inputPath, v, nil, convertID)
		}
	}

	needsReencode := reencode == "always" || hasCrop || hasSegments
	if !isAudioFormat && !needsReencode {
		probeCodec := probeVideoCodec(inputPath)
		codecCompat := map[string][]string{
			"mp4": {"h264", "avc", "hevc", "h265"}, "webm": {"vp8", "vp9", "av1"},
			"mkv": {"*"}, "mov": {"h264", "hevc", "prores"},
		}
		compat := codecCompat[format]
		isCompatible := sliceContains(compat, "*") || sliceContainsAny(compat, probeCodec)
		if reencode == "auto" && !isCompatible {
			needsReencode = true
		}
	}

	crfValues := map[string]int{"high": 18, "medium": 23, "low": 28}
	crf := crfValues[quality]
	if crf == 0 {
		crf = 23
	}

	buildSegArgs := func(segStart, segEnd float64, outFile string) []string {
		args := []string{"-y"}
		if segStart > 0 {
			args = append(args, "-ss", fmt.Sprintf("%.3f", segStart))
		}
		if segEnd < duration {
			args = append(args, "-to", fmt.Sprintf("%.3f", segEnd))
		}
		args = append(args, "-i", inputPath, "-threads", "0")

		if isAudioFormat {
			args = append(args, audioCodecArgs(format, audioBitrate)...)
			args = append(args, "-vn")
		} else if needsReencode {
			if cropFilter != "" {
				args = append(args, "-vf", cropFilter)
			}
			args = append(args, videoCodecArgs(format, crf)...)
		} else {
			args = append(args, "-codec", "copy")
		}

		if !isAudioFormat && (format == "mp4" || format == "mov") {
			args = append(args, "-movflags", "+faststart")
		}
		args = append(args, outFile)
		return args
	}

	cleanupOnError := func() {
		os.Remove(inputPath)
		os.Remove(outputPath)
		for _, c := range tempClips {
			os.Remove(c)
		}
		services.Global.DecrementJob("convert")
		services.Global.UnlinkJobFromClient(convertID)
	}

	if hasSegments {
		log.Printf("[%s] Processing %d segments\n", convertID, len(segments))
		job.SetProgressAndMessage(10, fmt.Sprintf("Processing segment 1/%d...", len(segments)))

		var clipPaths []string
		totalSegDuration := 0.0
		for _, s := range segments {
			totalSegDuration += s.End - s.Start
		}
		processedDuration := 0.0

		timeRegex := ffmpegTimeRegex

		for i, seg := range segments {
			clipPath := filepath.Join(config.TempDirs["convert"], fmt.Sprintf("%s-clip%d.%s", convertID, i, format))
			tempClips = append(tempClips, clipPath)
			clipPaths = append(clipPaths, clipPath)

			segDuration := seg.End - seg.Start
			segArgs := buildSegArgs(seg.Start, seg.End, clipPath)

			job.SetMessage(fmt.Sprintf("Processing segment %d/%d...", i+1, len(segments)))

			cmd := exec.Command("ffmpeg", segArgs...)
			stderrPipe, _ := cmd.StderrPipe()
			if err := cmd.Start(); err != nil {
				cleanupOnError()
				job.SetError(fmt.Sprintf("Segment %d failed", i+1))
				return nil
			}

			go func(segIdx int, segDur, procDur float64) {
				buf := make([]byte, 4096)
				for {
					n, err := stderrPipe.Read(buf)
					if n > 0 {
						msg := string(buf[:n])
						if m := timeRegex.FindStringSubmatch(msg); m != nil {
							h, _ := strconv.Atoi(m[1])
							min, _ := strconv.Atoi(m[2])
							sec, _ := strconv.ParseFloat(m[3], 64)
							ct := float64(h)*3600 + float64(min)*60 + sec
							segProgress := ct / segDur
							overall := 10 + ((procDur+segDur*segProgress)/totalSegDuration)*75
							if overall > 85 {
								overall = 85
							}
							job.SetProgressAndMessage(math.Round(overall), fmt.Sprintf("Segment %d/%d... %d%%", segIdx+1, len(segments), int(segProgress*100)))
						}
					}
					if err != nil {
						break
					}
				}
			}(i, segDuration, processedDuration)

			if err := cmd.Wait(); err != nil {
				cleanupOnError()
				job.SetError(fmt.Sprintf("Segment %d failed", i+1))
				return nil
			}
			processedDuration += segDuration
		}

		concatListPath := filepath.Join(config.TempDirs["convert"], convertID+"-concat.txt")
		tempClips = append(tempClips, concatListPath)
		var concatContent strings.Builder
		for _, p := range clipPaths {
			concatContent.WriteString(fmt.Sprintf("file '%s'\n", strings.ReplaceAll(p, "'", "'\\''")))
		}
		os.WriteFile(concatListPath, []byte(concatContent.String()), 0644)

		job.SetProgressAndMessage(90, "Joining segments...")

		concatArgs := []string{"-y", "-f", "concat", "-safe", "0", "-i", concatListPath, "-c", "copy"}
		if format == "mp4" || format == "mov" {
			concatArgs = append(concatArgs, "-movflags", "+faststart")
		}
		concatArgs = append(concatArgs, outputPath)

		cmd := exec.Command("ffmpeg", concatArgs...)
		if err := cmd.Run(); err != nil {
			cleanupOnError()
			job.SetError("Failed to join segments")
			return nil
		}

		for _, c := range tempClips {
			os.Remove(c)
		}
	} else {
		validStartTime := util.ValidateTimeParam(startTime)
		validEndTime := util.ValidateTimeParam(endTime)

		finalArgs := []string{"-y"}
		if validStartTime != "" {
			finalArgs = append(finalArgs, "-ss", validStartTime)
		}
		if validEndTime != "" {
			finalArgs = append(finalArgs, "-to", validEndTime)
		}
		finalArgs = append(finalArgs, "-i", inputPath, "-threads", "0")

		if isAudioFormat {
			finalArgs = append(finalArgs, audioCodecArgs(format, audioBitrate)...)
			finalArgs = append(finalArgs, "-vn")
		} else if needsReencode {
			if cropFilter != "" {
				finalArgs = append(finalArgs, "-vf", cropFilter)
			}
			finalArgs = append(finalArgs, videoCodecArgs(format, crf)...)
		} else {
			finalArgs = append(finalArgs, "-codec", "copy")
		}

		if !isAudioFormat && (format == "mp4" || format == "mov") {
			finalArgs = append(finalArgs, "-movflags", "+faststart")
		}
		finalArgs = append(finalArgs, outputPath)

		job.SetProgressAndMessage(10, "Converting...")

		cmd := exec.Command("ffmpeg", finalArgs...)
		stderrPipe, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			cleanupOnError()
			job.SetError("Conversion failed")
			return nil
		}

		timeRegex := ffmpegTimeRegex
		speedRegex := ffmpegSpeedRegex

		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := stderrPipe.Read(buf)
				if n > 0 {
					msg := string(buf[:n])
					if m := timeRegex.FindStringSubmatch(msg); m != nil {
						h, _ := strconv.Atoi(m[1])
						min, _ := strconv.Atoi(m[2])
						sec, _ := strconv.ParseFloat(m[3], 64)
						currentTime := float64(h)*3600 + float64(min)*60 + sec
						progress := 10 + (currentTime/duration)*85
						if progress > 95 {
							progress = 95
						}

						var eta string
						if sm := speedRegex.FindStringSubmatch(msg); sm != nil {
							speed, _ := strconv.ParseFloat(sm[1], 64)
							if speed > 0 {
								eta = util.FormatETA((duration - currentTime) / speed)
							}
						}

						statusMsg := fmt.Sprintf("Converting... %d%%", int(progress))
						if eta != "" {
							statusMsg = fmt.Sprintf("Converting... %d%% (ETA: %s)", int(progress), eta)
						}
						job.SetProgressAndMessage(math.Round(progress), statusMsg)
					}
				}
				if err != nil {
					break
				}
			}
		}()

		if err := cmd.Wait(); err != nil {
			cleanupOnError()
			job.SetError("Conversion failed")
			return nil
		}
	}

	os.Remove(inputPath)

	baseName := strings.TrimSuffix(filepath.Base(originalName), filepath.Ext(originalName))
	outputFilename := util.SanitizeFilename(baseName) + "." + format
	mimeType := getMimeForFormat(format, isAudioFormat)

	log.Printf("[%s] Async conversion complete\n", convertID)

	job.Lock()
	job.Status = "complete"
	job.Progress = 100
	job.Message = "Conversion complete!"
	job.OutputPath = outputPath
	job.OutputFilename = outputFilename
	job.MimeType = mimeType
	job.Unlock()

	services.Global.DecrementJob("convert")
	services.Global.UnlinkJobFromClient(convertID)
	return nil
}

func handleCompressAsync(inputPath, originalName, clientID, targetSizeStr, durationStr,
	mode, quality, preset, denoise string, shouldDownscale bool, jobID string) error {

	job := services.Global.GetAsyncJob(jobID)
	if job == nil {
		return nil
	}

	targetMB, _ := strconv.ParseFloat(targetSizeStr, 64)
	videoDuration, _ := strconv.ParseFloat(durationStr, 64)

	compressID := jobID
	outputPath := filepath.Join(config.TempDirs["compress"], compressID+"-compressed.mp4")
	passLogFile := filepath.Join(config.TempDirs["compress"], compressID+"-pass")

	if math.IsNaN(targetMB) || targetMB <= 0 {
		os.Remove(inputPath)
		job.SetError("Invalid target size")
		return nil
	}
	if math.IsNaN(videoDuration) || videoDuration <= 0 {
		os.Remove(inputPath)
		job.SetError("Invalid video duration")
		return nil
	}

	compressCheck := services.Global.CanStartJob("compress")
	if !compressCheck.OK {
		os.Remove(inputPath)
		job.SetError(compressCheck.Reason)
		return nil
	}

	if clientID != "" {
		services.Global.RegisterClient(clientID)
		services.Global.LinkJobToClient(compressID, clientID)
	}

	log.Printf("[%s] Async compress | Mode: %s | Preset: %s\n", compressID, mode, preset)

	processInfo := &services.ProcessInfo{TempFile: outputPath, JobType: "compress"}
	services.Global.SetProcess(compressID, processInfo)

	cleanupOnError := func() {
		os.Remove(inputPath)
		os.Remove(outputPath)
		os.Remove(passLogFile + "-0.log")
		os.Remove(passLogFile + "-0.log.mbtree")
		services.Global.ReleaseJob(compressID)
	}

	job.SetMessage("Analyzing video...")

	if !util.ValidateVideoFile(inputPath) {
		cleanupOnError()
		job.SetError("File does not contain valid video")
		return nil
	}

	probe := probeVideoFull(inputPath)
	actualDuration := videoDuration
	if actualDuration <= 0 {
		actualDuration = probe.duration
	}
	sourceFileSizeMB := float64(fileSizeBytes(inputPath)) / (1024 * 1024)
	sourceBitrateMbps := (sourceFileSizeMB * 8) / actualDuration

	presetConfig, ok := config.CompressionPresets[preset]
	if !ok {
		presetConfig = config.CompressionPresets["balanced"]
	}
	denoiseFilter := util.GetDenoiseFilter(denoise, probe.height, sourceBitrateMbps, presetConfig.Denoise)
	var downscaleWidth int
	if shouldDownscale {
		downscaleWidth = util.GetDownscaleResolution(probe.width, probe.height)
	}

	if mode == "quality" {
		crf := presetConfig.CRF[quality]
		vfArg := util.BuildVideoFilters(denoiseFilter, downscaleWidth, probe.width)

		job.SetProgressAndMessage(5, fmt.Sprintf("Encoding (%s)...", preset))

		err := runCrfEncodeAsync(inputPath, outputPath, crf, presetConfig.FFmpegPreset, vfArg,
			presetConfig.X264Params, processInfo, actualDuration, job)
		if err != nil {
			cleanupOnError()
			job.SetError(err.Error())
			return nil
		}
	} else {
		if sourceFileSizeMB <= targetMB {
			job.SetProgressAndMessage(50, "Already under target...")
			cmd := exec.Command("ffmpeg", "-y", "-i", inputPath, "-c:v", "copy", "-c:a", "copy", "-movflags", "+faststart", outputPath)
			processInfo.SetCmd(cmd)
			if err := cmd.Run(); err != nil {
				cleanupOnError()
				job.SetError("Remux failed")
				return nil
			}
		} else {
			videoBitrateK := util.CalculateTargetBitrate(targetMB, actualDuration, 96)
			resolution := util.SelectResolution(probe.width, probe.height, videoBitrateK)
			scaleWidth := downscaleWidth
			if scaleWidth == 0 && resolution.NeedsScale {
				scaleWidth = resolution.Width
			}
			vfArg := util.BuildVideoFilters(denoiseFilter, scaleWidth, probe.width)

			err := runTwoPassEncodeAsync(inputPath, outputPath, passLogFile, videoBitrateK,
				presetConfig.FFmpegPreset, vfArg, presetConfig.X264Params,
				processInfo, actualDuration, job)
			if err != nil {
				cleanupOnError()
				job.SetError(err.Error())
				return nil
			}
		}
	}

	os.Remove(inputPath)
	os.Remove(passLogFile + "-0.log")
	os.Remove(passLogFile + "-0.log.mbtree")

	stat, err := os.Stat(outputPath)
	if err != nil {
		cleanupOnError()
		job.SetError("output file not found after compression")
		return nil
	}
	baseName := strings.TrimSuffix(filepath.Base(originalName), filepath.Ext(originalName))
	outputFilename := util.SanitizeFilename(baseName) + "_compressed.mp4"

	log.Printf("[%s] Complete: %.2fMB\n", compressID, float64(stat.Size())/(1024*1024))

	job.Lock()
	job.Status = "complete"
	job.Progress = 100
	job.Message = "Complete!"
	job.OutputPath = outputPath
	job.OutputFilename = outputFilename
	job.MimeType = "video/mp4"
	job.Unlock()

	services.Global.ReleaseJob(compressID)
	return nil
}

func runCrfEncode(inputPath, outputPath string, crf int, ffmpegPreset, vfArg, x264Params string,
	processInfo *services.ProcessInfo, compressID string, duration float64) error {

	args := []string{"-y", "-i", inputPath, "-threads", "0"}
	if vfArg != "" {
		args = append(args, "-vf", vfArg)
	}
	args = append(args,
		"-c:v", "libx264", "-preset", ffmpegPreset, "-crf", strconv.Itoa(crf),
		"-pix_fmt", "yuv420p", "-profile:v", "high", "-level:v", "4.2",
		"-x264-params", x264Params,
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", outputPath)

	cmd := exec.Command("ffmpeg", args...)
	processInfo.SetCmd(cmd)

	stderrPipe, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}

	timeRegex := ffmpegTimeRegex
	speedRegex := ffmpegSpeedRegex
	lastProgress := 0.0

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				msg := string(buf[:n])
				if m := timeRegex.FindStringSubmatch(msg); m != nil {
					h, _ := strconv.Atoi(m[1])
					min, _ := strconv.Atoi(m[2])
					sec, _ := strconv.ParseFloat(m[3], 64)
					currentTime := float64(h)*3600 + float64(min)*60 + sec
					progress := (currentTime / duration) * 95
					if progress > 95 {
						progress = 95
					}

					var eta string
					if sm := speedRegex.FindStringSubmatch(msg); sm != nil {
						speed, _ := strconv.ParseFloat(sm[1], 64)
						if speed > 0 {
							eta = util.FormatETA((duration - currentTime) / speed)
						}
					}

					if progress > lastProgress+2 {
						lastProgress = progress
						statusMsg := fmt.Sprintf("Encoding... %d%%", int(progress))
						if eta != "" {
							statusMsg = fmt.Sprintf("Encoding... %d%% (ETA: %s)", int(progress), eta)
						}
						services.Global.SendProgressWithPercent(compressID, "compressing", statusMsg, progress)
					}
				}
			}
			if err != nil {
				break
			}
		}
	}()

	err := cmd.Wait()
	if processInfo.IsCancelled() {
		return fmt.Errorf("Cancelled")
	}
	return err
}

func runCrfEncodeAsync(inputPath, outputPath string, crf int, ffmpegPreset, vfArg, x264Params string,
	processInfo *services.ProcessInfo, duration float64, job *services.AsyncJob) error {

	args := []string{"-y", "-i", inputPath, "-threads", "0"}
	if vfArg != "" {
		args = append(args, "-vf", vfArg)
	}
	args = append(args,
		"-c:v", "libx264", "-preset", ffmpegPreset, "-crf", strconv.Itoa(crf),
		"-pix_fmt", "yuv420p", "-profile:v", "high", "-level:v", "4.2",
		"-x264-params", x264Params,
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", outputPath)

	cmd := exec.Command("ffmpeg", args...)
	processInfo.SetCmd(cmd)

	stderrPipe, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}

	timeRegex := ffmpegTimeRegex
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				if m := timeRegex.FindStringSubmatch(string(buf[:n])); m != nil {
					h, _ := strconv.Atoi(m[1])
					min, _ := strconv.Atoi(m[2])
					sec, _ := strconv.ParseFloat(m[3], 64)
					currentTime := float64(h)*3600 + float64(min)*60 + sec
					progress := (currentTime / duration) * 95
					if progress > 95 {
						progress = 95
					}
					job.SetProgressAndMessage(math.Round(progress), fmt.Sprintf("Encoding... %d%%", int(progress)))
				}
			}
			if err != nil {
				break
			}
		}
	}()

	err := cmd.Wait()
	if processInfo.IsCancelled() {
		return fmt.Errorf("Cancelled")
	}
	return err
}

func runTwoPassEncode(inputPath, outputPath, passLogFile string, videoBitrateK int,
	ffmpegPreset, vfArg, x264Params string,
	processInfo *services.ProcessInfo, compressID string, duration float64) error {

	maxrateK := int(float64(videoBitrateK) * 1.5)
	bufsizeK := videoBitrateK * 2

	services.Global.SendProgressWithPercent(compressID, "compressing", "Pass 1/2 - Analyzing...", 5)

	pass1Args := []string{"-y", "-i", inputPath, "-threads", "0"}
	if vfArg != "" {
		pass1Args = append(pass1Args, "-vf", vfArg)
	}
	pass1Args = append(pass1Args,
		"-c:v", "libx264", "-preset", ffmpegPreset,
		"-b:v", fmt.Sprintf("%dk", videoBitrateK),
		"-maxrate", fmt.Sprintf("%dk", maxrateK),
		"-bufsize", fmt.Sprintf("%dk", bufsizeK),
		"-pix_fmt", "yuv420p", "-profile:v", "high", "-level:v", "4.2",
		"-x264-params", x264Params,
		"-pass", "1", "-passlogfile", passLogFile,
		"-an", "-f", "null", os.DevNull)

	if err := runPassWithProgress(pass1Args, processInfo, compressID, duration, 0, 45, "Pass 1/2"); err != nil {
		return err
	}
	if processInfo.IsCancelled() {
		return fmt.Errorf("Cancelled")
	}

	services.Global.SendProgressWithPercent(compressID, "compressing", "Pass 2/2 - Encoding...", 50)

	pass2Args := []string{"-y", "-i", inputPath, "-threads", "0"}
	if vfArg != "" {
		pass2Args = append(pass2Args, "-vf", vfArg)
	}
	pass2Args = append(pass2Args,
		"-c:v", "libx264", "-preset", ffmpegPreset,
		"-b:v", fmt.Sprintf("%dk", videoBitrateK),
		"-maxrate", fmt.Sprintf("%dk", maxrateK),
		"-bufsize", fmt.Sprintf("%dk", bufsizeK),
		"-pix_fmt", "yuv420p", "-profile:v", "high", "-level:v", "4.2",
		"-x264-params", x264Params,
		"-pass", "2", "-passlogfile", passLogFile,
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", outputPath)

	return runPassWithProgress(pass2Args, processInfo, compressID, duration, 50, 45, "Pass 2/2")
}

func runTwoPassEncodeAsync(inputPath, outputPath, passLogFile string, videoBitrateK int,
	ffmpegPreset, vfArg, x264Params string,
	processInfo *services.ProcessInfo, duration float64, job *services.AsyncJob) error {

	maxrateK := int(float64(videoBitrateK) * 1.5)
	bufsizeK := videoBitrateK * 2

	job.SetProgressAndMessage(5, "Pass 1/2 - Analyzing...")

	pass1Args := []string{"-y", "-i", inputPath, "-threads", "0"}
	if vfArg != "" {
		pass1Args = append(pass1Args, "-vf", vfArg)
	}
	pass1Args = append(pass1Args,
		"-c:v", "libx264", "-preset", ffmpegPreset,
		"-b:v", fmt.Sprintf("%dk", videoBitrateK),
		"-maxrate", fmt.Sprintf("%dk", maxrateK),
		"-bufsize", fmt.Sprintf("%dk", bufsizeK),
		"-pix_fmt", "yuv420p", "-profile:v", "high", "-level:v", "4.2",
		"-x264-params", x264Params,
		"-pass", "1", "-passlogfile", passLogFile,
		"-an", "-f", "null", os.DevNull)

	if err := runPassWithJobProgress(pass1Args, processInfo, duration, 0, 45, "Pass 1/2", job); err != nil {
		return err
	}
	if processInfo.IsCancelled() {
		return fmt.Errorf("Cancelled")
	}

	job.SetProgressAndMessage(50, "Pass 2/2 - Encoding...")

	pass2Args := []string{"-y", "-i", inputPath, "-threads", "0"}
	if vfArg != "" {
		pass2Args = append(pass2Args, "-vf", vfArg)
	}
	pass2Args = append(pass2Args,
		"-c:v", "libx264", "-preset", ffmpegPreset,
		"-b:v", fmt.Sprintf("%dk", videoBitrateK),
		"-maxrate", fmt.Sprintf("%dk", maxrateK),
		"-bufsize", fmt.Sprintf("%dk", bufsizeK),
		"-pix_fmt", "yuv420p", "-profile:v", "high", "-level:v", "4.2",
		"-x264-params", x264Params,
		"-pass", "2", "-passlogfile", passLogFile,
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", outputPath)

	return runPassWithJobProgress(pass2Args, processInfo, duration, 50, 45, "Pass 2/2", job)
}

func runPassWithProgress(args []string, processInfo *services.ProcessInfo, compressID string,
	duration, baseProgress, progressRange float64, label string) error {

	cmd := exec.Command("ffmpeg", args...)
	processInfo.SetCmd(cmd)

	stderrPipe, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}

	timeRegex := ffmpegTimeRegex
	speedRegex := ffmpegSpeedRegex
	lastProgress := baseProgress

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				msg := string(buf[:n])
				if m := timeRegex.FindStringSubmatch(msg); m != nil {
					h, _ := strconv.Atoi(m[1])
					min, _ := strconv.Atoi(m[2])
					sec, _ := strconv.ParseFloat(m[3], 64)
					currentTime := float64(h)*3600 + float64(min)*60 + sec
					progress := baseProgress + (currentTime/duration)*progressRange
					if progress > baseProgress+progressRange {
						progress = baseProgress + progressRange
					}

					var eta string
					if sm := speedRegex.FindStringSubmatch(msg); sm != nil {
						speed, _ := strconv.ParseFloat(sm[1], 64)
						if speed > 0 {
							eta = util.FormatETA((duration - currentTime) / speed)
						}
					}

					if progress > lastProgress+2 {
						lastProgress = progress
						pct := int((progress - baseProgress) / progressRange * 100)
						statusMsg := fmt.Sprintf("%s - %d%%", label, pct)
						if eta != "" {
							statusMsg = fmt.Sprintf("%s - %d%% (ETA: %s)", label, pct, eta)
						}
						services.Global.SendProgressWithPercent(compressID, "compressing", statusMsg, progress)
					}
				}
			}
			if err != nil {
				break
			}
		}
	}()

	err := cmd.Wait()
	if processInfo.IsCancelled() {
		return fmt.Errorf("Cancelled")
	}
	return err
}

func runPassWithJobProgress(args []string, processInfo *services.ProcessInfo,
	duration, baseProgress, progressRange float64, label string, job *services.AsyncJob) error {

	cmd := exec.Command("ffmpeg", args...)
	processInfo.SetCmd(cmd)

	stderrPipe, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return err
	}

	timeRegex := ffmpegTimeRegex
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 {
				if m := timeRegex.FindStringSubmatch(string(buf[:n])); m != nil {
					h, _ := strconv.Atoi(m[1])
					min, _ := strconv.Atoi(m[2])
					sec, _ := strconv.ParseFloat(m[3], 64)
					currentTime := float64(h)*3600 + float64(min)*60 + sec
					progress := baseProgress + (currentTime/duration)*progressRange
					if progress > baseProgress+progressRange {
						progress = baseProgress + progressRange
					}
					pct := int((progress - baseProgress) / progressRange * 100)
					job.SetProgressAndMessage(math.Round(progress), fmt.Sprintf("%s - %d%%", label, pct))
				}
			}
			if err != nil {
				break
			}
		}
	}()

	err := cmd.Wait()
	if processInfo.IsCancelled() {
		return fmt.Errorf("Cancelled")
	}
	return err
}

type rawCropParams struct {
	X, Y, W, H int
}

func buildCropFilter(inputPath, cropRatio string, rawCrop *rawCropParams, logID string) (string, error) {
	if rawCrop != nil && rawCrop.W > 0 && rawCrop.H > 0 {
		probe := probeVideoFull(inputPath)
		if rawCrop.X+rawCrop.W > probe.width || rawCrop.Y+rawCrop.H > probe.height {
			log.Printf("[%s] Crop skipped: crop rect exceeds video bounds\n", logID)
			return "", nil
		}
		filter := fmt.Sprintf("crop=%d:%d:%d:%d", rawCrop.W, rawCrop.H, rawCrop.X, rawCrop.Y)
		log.Printf("[%s] Crop (raw): %s\n", logID, filter)
		return filter, nil
	}

	if cropRatio == "" {
		return "", nil
	}

	probe := probeVideoFull(inputPath)
	parts := strings.Split(cropRatio, ":")
	if len(parts) != 2 {
		return "", nil
	}
	ratioW, _ := strconv.Atoi(parts[0])
	ratioH, _ := strconv.Atoi(parts[1])
	if probe.width == 0 || probe.height == 0 || ratioW == 0 || ratioH == 0 {
		return "", nil
	}

	var cw, ch, cx, cy int
	if float64(probe.width)/float64(probe.height) > float64(ratioW)/float64(ratioH) {
		ch = probe.height - (probe.height % 2)
		cw = int(math.Floor(float64(ch) * float64(ratioW) / float64(ratioH)))
		cw = cw - (cw % 2)
		cx = (probe.width - cw) / 2
		cy = (probe.height - ch) / 2
	} else {
		cw = probe.width - (probe.width % 2)
		ch = int(math.Floor(float64(cw) * float64(ratioH) / float64(ratioW)))
		ch = ch - (ch % 2)
		cx = (probe.width - cw) / 2
		cy = (probe.height - ch) / 2
	}

	filter := fmt.Sprintf("crop=%d:%d:%d:%d", cw, ch, cx, cy)
	log.Printf("[%s] Crop: %s -> %s\n", logID, cropRatio, filter)
	return filter, nil
}

type videoProbe struct {
	duration float64
	width    int
	height   int
	codec    string
}

func probeVideoFull(inputPath string) videoProbe {
	cmd := exec.Command("ffprobe",
		"-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height,codec_name,r_frame_rate:format=duration",
		"-of", "json", inputPath)
	out, err := cmd.Output()
	if err != nil {
		return videoProbe{60, 1920, 1080, "unknown"}
	}

	var parsed struct {
		Streams []struct {
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			CodecName string `json:"codec_name"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if json.Unmarshal(out, &parsed) != nil {
		return videoProbe{60, 1920, 1080, "unknown"}
	}

	dur, _ := strconv.ParseFloat(parsed.Format.Duration, 64)
	if dur <= 0 {
		dur = 60
	}
	w, h := 1920, 1080
	codec := "unknown"
	if len(parsed.Streams) > 0 {
		if parsed.Streams[0].Width > 0 {
			w = parsed.Streams[0].Width
		}
		if parsed.Streams[0].Height > 0 {
			h = parsed.Streams[0].Height
		}
		if parsed.Streams[0].CodecName != "" {
			codec = parsed.Streams[0].CodecName
		}
	}
	return videoProbe{dur, w, h, codec}
}

func probeVideoCodec(inputPath string) string {
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name", "-of", "csv=p=0", inputPath)
	out, _ := cmd.Output()
	return strings.ToLower(strings.TrimSpace(string(out)))
}

func probeVideoInfo(filePath string) (float64, int, int) {
	cmd := exec.Command("ffprobe",
		"-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height:format=duration",
		"-of", "json", filePath)
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, 0
	}

	var parsed struct {
		Streams []struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if json.Unmarshal(out, &parsed) != nil {
		return 0, 0, 0
	}

	dur, _ := strconv.ParseFloat(parsed.Format.Duration, 64)
	w, h := 0, 0
	if len(parsed.Streams) > 0 {
		w = parsed.Streams[0].Width
		h = parsed.Streams[0].Height
	}
	return dur, w, h
}

func isAudioFmt(format string) bool {
	return format == "mp3" || format == "m4a" || format == "opus" || format == "wav" || format == "flac"
}

func videoCodecArgs(format string, crf int) []string {
	switch format {
	case "webm":
		return []string{"-c:v", "libvpx-vp9", "-crf", strconv.Itoa(crf), "-b:v", "0",
			"-pix_fmt", "yuv420p", "-c:a", "libopus", "-b:a", "128k"}
	case "mkv":
		return []string{"-c:v", "libx264", "-preset", "medium", "-crf", strconv.Itoa(crf),
			"-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "128k"}
	default:
		return []string{"-c:v", "libx264", "-preset", "medium", "-crf", strconv.Itoa(crf),
			"-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "128k"}
	}
}

func audioCodecArgs(format, audioBitrate string) []string {
	switch format {
	case "mp3":
		return []string{"-codec:a", "libmp3lame", "-b:a", audioBitrate + "k"}
	case "m4a":
		return []string{"-codec:a", "aac", "-b:a", audioBitrate + "k"}
	case "opus":
		return []string{"-codec:a", "libopus", "-b:a", "128k"}
	case "wav":
		return []string{"-codec:a", "pcm_s16le"}
	case "flac":
		return []string{"-codec:a", "flac"}
	default:
		return []string{"-codec:a", "aac", "-b:a", "128k"}
	}
}

func getMimeForFormat(format string, isAudio bool) string {
	if isAudio {
		if m, ok := config.AudioMIMEs[format]; ok {
			return m
		}
		return "audio/mpeg"
	}
	if m, ok := config.ContainerMIMEs[format]; ok {
		return m
	}
	return "video/mp4"
}

func sliceContains(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}

func sliceContainsAny(s []string, substr string) bool {
	for _, item := range s {
		if strings.Contains(substr, item) {
			return true
		}
	}
	return false
}

func defaultStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func isHeaderSent(w http.ResponseWriter) bool {
	return false
}

func fileSizeBytes(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func isTruthy(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true"
	default:
		return false
	}
}

func compressError(w http.ResponseWriter, compressID string, processInfo *services.ProcessInfo, err error,
	inputPath, outputPath, passLogFile string) {
	log.Printf("[%s] Error: %s\n", compressID, err.Error())
	alerts.CompressionFailed(compressID, err)
	services.Global.ReleaseJob(compressID)
	go func() {
		time.Sleep(2 * time.Second)
		util.CleanupJobFiles(compressID)
	}()
	os.Remove(inputPath)
	os.Remove(outputPath)
	os.Remove(passLogFile + "-0.log")
	os.Remove(passLogFile + "-0.log.mbtree")
	if !processInfo.IsCancelled() {
		services.Global.SendProgressSimple(compressID, "error", err.Error())
	}
	respondJSON(w, 500, map[string]string{"error": err.Error()})
}

func StartChunkedUploadCleanup() {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			services.Global.CleanupExpiredChunkedUploads()
		}
	}()
}
