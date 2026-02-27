package routes

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

var (
	allowedOutputModes = []string{"subtitles", "captions", "text"}
	localModels        = []string{"tiny", "base", "small", "medium"}
	apiModels          = []string{"large"}
	allowedModels      = append(localModels, apiModels...)
	allowedSubFormats  = []string{"srt", "ass"}
	langRegex          = regexp.MustCompile(`^[a-zA-Z]{2,5}$`)
)

var WhisperScript string

func init() {
	WhisperScript = os.Getenv("WHISPER_SCRIPT")
	if WhisperScript == "" {
		exe, err := os.Executable()
		if err == nil {
			dir := filepath.Dir(exe)
			candidate := filepath.Join(dir, "whisper.py")
			if _, err := os.Stat(candidate); err == nil {
				WhisperScript = candidate
			}
		}
		if WhisperScript == "" {
			WhisperScript = "whisper.py"
		}
	}
}

type transcribeOpts struct {
	OutputMode         string
	Model              string
	SubtitleFormat     string
	Language           string
	CaptionSize        int
	MaxWordsPerCaption int
	MaxCharsPerLine    int
	MinDuration        float64
	CaptionGap         float64
	ClientID           string
}

func extractTranscribeOpts(r *http.Request) transcribeOpts {
	return transcribeOpts{
		OutputMode:         formValueOr(r, "outputMode", "text"),
		Model:              formValueOr(r, "model", "base"),
		SubtitleFormat:     formValueOr(r, "subtitleFormat", "srt"),
		Language:           r.FormValue("language"),
		CaptionSize:        intFormValue(r, "captionSize", 72),
		MaxWordsPerCaption: intFormValue(r, "maxWordsPerCaption", 0),
		MaxCharsPerLine:    intFormValue(r, "maxCharsPerLine", 0),
		MinDuration:        floatFormValue(r, "minDuration", 0),
		CaptionGap:         floatFormValue(r, "captionGap", 0),
		ClientID:           r.FormValue("clientId"),
	}
}

func TranscribeRoutes(r chi.Router) {
	r.Post("/api/transcribe", handleTranscribe)
	r.Post("/api/transcribe-chunked", handleTranscribeChunked)
}

func handleTranscribe(w http.ResponseWriter, r *http.Request) {
	filePath, originalName, err := saveUploadedFile(r, w, "file")
	if err != nil {
		respondJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}

	clientID := r.FormValue("clientId")
	if clientID != "" {
		if services.Global.GetClientJobCount(clientID) >= config.MaxJobsPerClient {
			os.Remove(filePath)
			respondJSON(w, 429, map[string]string{
				"error": fmt.Sprintf("Too many active jobs. Maximum %d concurrent jobs per user.", config.MaxJobsPerClient),
			})
			return
		}
	}

	jobID := uuid.New().String()
	services.Global.SetAsyncJob(jobID, &services.AsyncJob{
		Status:    "processing",
		Progress:  0,
		Message:   "Starting transcription...",
		CreatedAt: time.Now(),
	})

	opts := extractTranscribeOpts(r)
	respondJSON(w, 200, map[string]string{"jobId": jobID})

	go func() {
		if err := handleTranscribeAsync(filePath, originalName, opts, jobID); err != nil {
			log.Printf("[AsyncJob] Transcribe job %s failed: %s\n", jobID, err.Error())
			alerts.TranscriptionFailed(jobID, err)
			job := services.Global.GetAsyncJob(jobID)
			if job != nil {
				job.SetError(err.Error())
			}
		}
	}()
}

func handleTranscribeChunked(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FilePath string `json:"filePath"`
		FileName string `json:"fileName"`
		ClientID string `json:"clientId"`
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

	fileName := body.FileName
	if fileName == "" {
		fileName = "media"
	}

	jobID := uuid.New().String()
	services.Global.SetAsyncJob(jobID, &services.AsyncJob{
		Status:    "processing",
		Progress:  0,
		Message:   "Starting transcription...",
		CreatedAt: time.Now(),
	})

	opts := transcribeOpts{
		OutputMode:     "text",
		Model:          "base",
		SubtitleFormat: "srt",
		CaptionSize:    72,
		ClientID:       body.ClientID,
	}
	respondJSON(w, 200, map[string]string{"jobId": jobID})

	go func() {
		if err := handleTranscribeAsync(validPath, fileName, opts, jobID); err != nil {
			log.Printf("[AsyncJob] Transcribe job %s failed: %s\n", jobID, err.Error())
			alerts.TranscriptionFailed(jobID, err)
			job := services.Global.GetAsyncJob(jobID)
			if job != nil {
				job.SetError(err.Error())
			}
		}
	}()
}

func handleTranscribeAsync(inputPath, originalName string, opts transcribeOpts, jobID string) error {
	job := services.Global.GetAsyncJob(jobID)
	if job == nil {
		return nil
	}

	outputMode := opts.OutputMode
	model := opts.Model
	subtitleFormat := opts.SubtitleFormat
	language := opts.Language
	captionSize := opts.CaptionSize
	maxWordsPerCaption := opts.MaxWordsPerCaption
	maxCharsPerLine := opts.MaxCharsPerLine
	minDuration := opts.MinDuration
	captionGap := opts.CaptionGap
	clientID := opts.ClientID

	if !contains(allowedOutputModes, outputMode) {
		job.SetError(fmt.Sprintf("Invalid output mode. Allowed: %s", strings.Join(allowedOutputModes, ", ")))
		return nil
	}
	if !contains(allowedModels, model) {
		job.SetError(fmt.Sprintf("Invalid model. Allowed: %s", strings.Join(allowedModels, ", ")))
		return nil
	}
	if contains(apiModels, model) && config.OpenAIAPIKey == "" {
		os.Remove(inputPath)
		job.SetError("Large model requires API configuration. Use a local model (tiny/base/small/medium).")
		return nil
	}
	if outputMode == "subtitles" && !contains(allowedSubFormats, subtitleFormat) {
		job.SetError(fmt.Sprintf("Invalid subtitle format. Allowed: %s", strings.Join(allowedSubFormats, ", ")))
		return nil
	}
	if language != "" && !langRegex.MatchString(language) {
		job.SetError("Invalid language code. Use 2-5 letter code (e.g. en, es, ja).")
		return nil
	}

	if outputMode != "text" {
		if captionSize != 72 && (captionSize < 40 || captionSize > 120) {
			job.SetError("captionSize must be an integer between 40 and 120.")
			return nil
		}
		if maxWordsPerCaption != 0 && (maxWordsPerCaption < 1 || maxWordsPerCaption > 20) {
			job.SetError("maxWordsPerCaption must be an integer between 1 and 20.")
			return nil
		}
		if maxCharsPerLine != 0 && (maxCharsPerLine < 10 || maxCharsPerLine > 80) {
			job.SetError("maxCharsPerLine must be an integer between 10 and 80.")
			return nil
		}
		if minDuration != 0 && (minDuration < 0.1 || minDuration > 5) {
			job.SetError("minDuration must be between 0.1 and 5 seconds.")
			return nil
		}
		if captionGap != 0 && (captionGap < 0 || captionGap > 1) {
			job.SetError("captionGap must be between 0 and 1 seconds.")
			return nil
		}
	}

	transcribeCheck := services.Global.CanStartJob("transcribe")
	if !transcribeCheck.OK {
		os.Remove(inputPath)
		job.SetError(transcribeCheck.Reason)
		return nil
	}

	transcribeID := jobID

	if clientID != "" {
		services.Global.RegisterClient(clientID)
		services.Global.LinkJobToClient(transcribeID, clientID)
	}

	log.Printf("[%s] Transcribing | Mode: %s | Model: %s\n", transcribeID, outputMode, model)

	processInfo := &services.ProcessInfo{JobType: "transcribe"}
	services.Global.SetProcess(transcribeID, processInfo)

	wavPath := filepath.Join(config.TempDirs["transcribe"], transcribeID+".wav")
	var whisperOutputFormat string
	switch outputMode {
	case "text":
		whisperOutputFormat = "txt"
	case "subtitles":
		whisperOutputFormat = subtitleFormat
	default:
		whisperOutputFormat = "ass"
	}
	whisperOutputPath := filepath.Join(config.TempDirs["transcribe"], transcribeID+"."+whisperOutputFormat)
	captionedPath := filepath.Join(config.TempDirs["transcribe"], transcribeID+"-captioned.mp4")

	cleanupAll := func() {
		os.Remove(inputPath)
		os.Remove(wavPath)
		os.Remove(whisperOutputPath)
		os.Remove(captionedPath)
		services.Global.DeleteProcess(transcribeID)
		services.Global.DecrementJob("transcribe")
		services.Global.UnlinkJobFromClient(transcribeID)
	}

	job.SetProgressAndMessage(1, "Analyzing file...")

	hasVideo := probeHasStream(inputPath, "v:0", "video")
	hasAudio := probeHasStream(inputPath, "a:0", "audio")

	if !hasAudio {
		cleanupAll()
		job.SetError("No audio found in file")
		return nil
	}
	if outputMode == "captions" && !hasVideo {
		cleanupAll()
		job.SetError("Captions mode requires a video file (no video stream found)")
		return nil
	}

	job.SetProgressAndMessage(2, "Extracting audio...")

	cmd := exec.Command("ffmpeg", "-y", "-i", inputPath, "-vn", "-acodec", "pcm_s16le", "-ar", "16000", "-ac", "1", wavPath)
	processInfo.SetCmd(cmd)
	if err := cmd.Run(); err != nil {
		if processInfo.IsCancelled() {
			cleanupAll()
			job.SetError("Cancelled")
			return nil
		}
		cleanupAll()
		return fmt.Errorf("Audio extraction failed: %w", err)
	}

	wavStat, err := os.Stat(wavPath)
	if err != nil || wavStat.Size() < 1000 {
		cleanupAll()
		job.SetError("No audio found in file")
		return nil
	}

	job.SetProgress(5)

	job.SetMessage("Starting transcription...")

	whisperArgs := []string{
		WhisperScript,
		"--input", wavPath,
		"--model", model,
		"--output-format", whisperOutputFormat,
		"--output", whisperOutputPath,
	}
	if language != "" {
		whisperArgs = append(whisperArgs, "--language", language)
	}
	if contains(apiModels, model) {
		whisperArgs = append(whisperArgs, "--use-api")
	}
	if outputMode != "text" {
		if captionSize != 72 {
			whisperArgs = append(whisperArgs, "--font-size", strconv.Itoa(captionSize))
		}
		if maxWordsPerCaption > 0 {
			whisperArgs = append(whisperArgs, "--max-words-per-caption", strconv.Itoa(maxWordsPerCaption))
		}
		if maxCharsPerLine > 0 {
			whisperArgs = append(whisperArgs, "--max-chars-per-line", strconv.Itoa(maxCharsPerLine))
		}
		if minDuration > 0 {
			whisperArgs = append(whisperArgs, "--min-duration", fmt.Sprintf("%.2f", minDuration))
		}
		if captionGap > 0 {
			whisperArgs = append(whisperArgs, "--gap", fmt.Sprintf("%.2f", captionGap))
		}
	}

	whisperCmd := exec.Command("python3", whisperArgs...)
	processInfo.SetCmd(whisperCmd)

	whisperStdout, _ := whisperCmd.StdoutPipe()
	whisperStderr, _ := whisperCmd.StderrPipe()

	if err := whisperCmd.Start(); err != nil {
		cleanupAll()
		return fmt.Errorf("Failed to start whisper: %w", err)
	}

	go func() {
		buf := make([]byte, 4096)
		var partial string
		for {
			n, err := whisperStderr.Read(buf)
			if n > 0 {
				partial += string(buf[:n])
				lines := strings.Split(partial, "\n")
				partial = lines[len(lines)-1]

				for _, line := range lines[:len(lines)-1] {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					var progress struct {
						Progress float64 `json:"progress"`
						Message  string  `json:"message"`
					}
					if json.Unmarshal([]byte(line), &progress) == nil && progress.Progress > 0 {
						mapped := 5 + (progress.Progress/95)*80
						if mapped > 85 {
							mapped = 85
						}
						msg := "Transcribing..."
						if progress.Message != "" {
							msg = progress.Message
						}
						job.SetProgressAndMessage(mapped, msg)
					}
				}
			}
			if err != nil {
				break
			}
		}
	}()

	whisperStdoutBytes, _ := io.ReadAll(whisperStdout)
	whisperErr := whisperCmd.Wait()

	if processInfo.IsCancelled() {
		cleanupAll()
		job.SetError("Cancelled")
		return nil
	}

	var whisperResult struct {
		Success      bool   `json:"success"`
		Error        string `json:"error"`
		SegmentCount int    `json:"segmentCount"`
		Language     string `json:"language"`
	}
	if err := json.Unmarshal(whisperStdoutBytes, &whisperResult); err != nil || !whisperResult.Success {
		cleanupAll()
		errMsg := "Transcription failed"
		if whisperResult.Error != "" {
			errMsg = whisperResult.Error
		} else if whisperErr != nil {
			errMsg = fmt.Sprintf("Whisper process exited with error: %v", whisperErr)
		}
		job.SetError(errMsg)
		return nil
	}

	log.Printf("[%s] Whisper done: %d segments, language: %s\n", transcribeID, whisperResult.SegmentCount, whisperResult.Language)

	baseName := strings.TrimSuffix(filepath.Base(originalName), filepath.Ext(originalName))
	safeName := util.SanitizeFilename(baseName)

	switch outputMode {
	case "text":
		job.SetProgressAndMessage(90, "Preparing transcript...")

		textContent, err := os.ReadFile(whisperOutputPath)
		if err != nil {
			cleanupAll()
			return fmt.Errorf("Failed to read transcript: %w", err)
		}

		job.Lock()
		job.Status = "complete"
		job.Progress = 100
		job.Message = "Transcription complete!"
		job.TextContent = string(textContent)
		job.OutputPath = whisperOutputPath
		job.OutputFilename = safeName + "_transcript.txt"
		job.MimeType = "text/plain"
		job.Unlock()

	case "subtitles":
		job.SetProgressAndMessage(90, "Preparing subtitles...")

		mimeType := "application/x-subrip"
		if subtitleFormat == "ass" {
			mimeType = "text/x-ssa"
		}

		job.Lock()
		job.Status = "complete"
		job.Progress = 100
		job.Message = "Transcription complete!"
		job.OutputPath = whisperOutputPath
		job.OutputFilename = safeName + "." + subtitleFormat
		job.MimeType = mimeType
		job.Unlock()

	case "captions":
		job.SetProgressAndMessage(86, "Burning captions into video...")

		duration := probeDuration(inputPath)

		escapedPath := strings.ReplaceAll(whisperOutputPath, `\`, `\\`)
		escapedPath = strings.ReplaceAll(escapedPath, `:`, `\:`)
		escapedPath = strings.ReplaceAll(escapedPath, `'`, `\'`)

		captionCmd := exec.Command("ffmpeg",
			"-y", "-i", inputPath,
			"-vf", "ass="+escapedPath,
			"-c:v", "libx264", "-preset", "medium", "-crf", "23",
			"-pix_fmt", "yuv420p",
			"-c:a", "aac", "-b:a", "128k",
			"-movflags", "+faststart",
			captionedPath,
		)
		processInfo.SetCmd(captionCmd)

		captionStderr, _ := captionCmd.StderrPipe()
		if err := captionCmd.Start(); err != nil {
			cleanupAll()
			return fmt.Errorf("Failed to start caption burn-in: %w", err)
		}

		go func() {
			buf := make([]byte, 4096)
			timeRegex := ffmpegTimeRegex
			for {
				n, err := captionStderr.Read(buf)
				if n > 0 {
					msg := string(buf[:n])
					if m := timeRegex.FindStringSubmatch(msg); m != nil {
						h, _ := strconv.Atoi(m[1])
						min, _ := strconv.Atoi(m[2])
						sec, _ := strconv.ParseFloat(m[3], 64)
						currentTime := float64(h)*3600 + float64(min)*60 + sec
						if duration > 0 {
							p := 86 + (currentTime/duration)*13
							if p > 99 {
								p = 99
							}
							job.SetProgressAndMessage(p, fmt.Sprintf("Burning captions... %d%%", int(currentTime/duration*100)))
						}
					}
				}
				if err != nil {
					break
				}
			}
		}()

		if err := captionCmd.Wait(); err != nil {
			if processInfo.IsCancelled() {
				cleanupAll()
				job.SetError("Cancelled")
				return nil
			}
			cleanupAll()
			return fmt.Errorf("Caption burn-in failed: %w", err)
		}

		job.Lock()
		job.Status = "complete"
		job.Progress = 100
		job.Message = "Captions burned in!"
		job.OutputPath = captionedPath
		job.OutputFilename = safeName + "_captioned.mp4"
		job.MimeType = "video/mp4"
		job.Unlock()
	}

	os.Remove(wavPath)
	os.Remove(inputPath)
	if outputMode == "captions" {
		os.Remove(whisperOutputPath)
	}

	services.Global.DeleteProcess(transcribeID)
	services.Global.DecrementJob("transcribe")
	services.Global.UnlinkJobFromClient(transcribeID)

	return nil
}

func probeHasStream(inputPath, streamSel, codecType string) bool {
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", streamSel,
		"-show_entries", "stream=codec_type", "-of", "csv=p=0", inputPath)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.TrimSpace(string(out)), codecType)
}

func probeDuration(inputPath string) float64 {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration",
		"-of", "csv=p=0", inputPath)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	var dur float64
	fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &dur)
	return dur
}

