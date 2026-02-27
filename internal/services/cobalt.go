package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/coah80/yoink/internal/config"
)

var ffmpegTimeRe = regexp.MustCompile(`time=(\d+):(\d+):(\d+\.?\d*)`)

type CobaltDownloadURL struct {
	URL      string
	Filename string
	Status   string
	APIURL   string
}

type CobaltMetadata struct {
	Title      string `json:"title"`
	Ext        string `json:"ext"`
	ID         string `json:"id"`
	Uploader   string `json:"uploader"`
	Duration   string `json:"duration"`
	Thumbnail  string `json:"thumbnail"`
	IsPlaylist bool   `json:"isPlaylist"`
	ViaCobalt  bool   `json:"viaCobalt"`
}

type CobaltDownloadResult struct {
	FilePath    string
	Ext         string
	DownloadURL string
}

type CobaltClipResult struct {
	FilePath string
	Ext      string
}

type CobaltDownloadOpts struct {
	OutputDir  string
	MaxRetries int
	RetryDelay time.Duration
}

func cobaltHeaders() map[string]string {
	h := map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	if config.CobaltAPIKey != "" {
		h["Authorization"] = "Api-Key " + config.CobaltAPIKey
	}
	return h
}

func cobaltPost(ctx context.Context, apiURL string, body map[string]interface{}) (map[string]interface{}, error) {
	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}
	for k, v := range cobaltHeaders() {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errData map[string]interface{}
		if json.Unmarshal(respBody, &errData) == nil {
			if errObj, ok := errData["error"].(map[string]interface{}); ok {
				if code, ok := errObj["code"].(string); ok {
					return nil, fmt.Errorf("%s", code)
				}
			}
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON response")
	}

	if data["status"] == "error" {
		if errObj, ok := data["error"].(map[string]interface{}); ok {
			if code, ok := errObj["code"].(string); ok {
				return nil, fmt.Errorf("%s", code)
			}
		}
		return nil, fmt.Errorf("Cobalt error")
	}

	return data, nil
}

func GetCobaltDownloadURL(ctx context.Context, videoURL string, isAudio bool, videoQuality string) (*CobaltDownloadURL, error) {
	if videoQuality == "" {
		videoQuality = "1080"
	}

	var lastErr error
	for _, apiURL := range config.CobaltAPIs {
		log.Printf("[Cobalt] Getting URL from: %s", apiURL)

		body := map[string]interface{}{
			"url":          videoURL,
			"downloadMode": "auto",
			"filenameStyle": "basic",
			"videoQuality": videoQuality,
		}
		if isAudio {
			body["downloadMode"] = "audio"
		}

		data, err := cobaltPost(ctx, apiURL, body)
		if err != nil {
			log.Printf("[Cobalt] %s failed: %s", apiURL, err)
			lastErr = err
			continue
		}

		var downloadURL string
		filename := "download"
		if fn, ok := data["filename"].(string); ok {
			filename = fn
		}

		status, _ := data["status"].(string)
		if status == "tunnel" || status == "redirect" {
			downloadURL, _ = data["url"].(string)
		} else if status == "picker" {
			if picker, ok := data["picker"].([]interface{}); ok && len(picker) > 0 {
				if first, ok := picker[0].(map[string]interface{}); ok {
					downloadURL, _ = first["url"].(string)
				}
			}
		}

		if downloadURL == "" {
			lastErr = fmt.Errorf("No download URL in response")
			continue
		}

		log.Printf("[Cobalt] Got %s URL from %s", status, apiURL)
		return &CobaltDownloadURL{
			URL:      downloadURL,
			Filename: filename,
			Status:   status,
			APIURL:   apiURL,
		}, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("All Cobalt instances failed")
}

func FetchMetadataViaCobalt(ctx context.Context, videoURL string) (*CobaltMetadata, error) {
	var lastErr error
	for _, apiURL := range config.CobaltAPIs {
		log.Printf("[Metadata] Trying Cobalt: %s", apiURL)

		body := map[string]interface{}{
			"url":          videoURL,
			"downloadMode": "auto",
			"filenameStyle": "basic",
			"videoQuality": "1080",
		}

		data, err := cobaltPost(ctx, apiURL, body)
		if err != nil {
			log.Printf("[Metadata] Cobalt %s failed: %s", apiURL, err)
			lastErr = err
			continue
		}

		filename := "download"
		if fn, ok := data["filename"].(string); ok {
			filename = fn
		}

		title := strings.TrimSuffix(filename, filepath.Ext(filename))
		if title == "" {
			title = "download"
		}
		ext := strings.TrimPrefix(filepath.Ext(filename), ".")
		if ext == "" {
			ext = "mp4"
		}

		log.Printf("[Metadata] Cobalt success via %s", apiURL)
		return &CobaltMetadata{
			Title:     title,
			Ext:       ext,
			ViaCobalt: true,
		}, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("All Cobalt instances failed")
}

func DownloadViaCobalt(ctx context.Context, videoURL, jobID string, isAudio bool, progressCb func(float64, int64, int64), opts CobaltDownloadOpts) (*CobaltDownloadResult, error) {
	if opts.OutputDir == "" {
		opts.OutputDir = config.TempDirs["download"]
	}
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 3
	}
	if opts.RetryDelay == 0 {
		opts.RetryDelay = 2 * time.Second
	}

	var lastErr error
	attemptCount := 0
	startTime := time.Now()

	for retry := 0; retry < opts.MaxRetries; retry++ {
		for _, apiURL := range config.CobaltAPIs {
			attemptCount++
			if ctx.Err() != nil {
				return nil, fmt.Errorf("Cancelled")
			}

			log.Printf("[Cobalt] [%s] Attempt %d - Trying: %s (retry %d/%d)", jobID, attemptCount, apiURL, retry+1, opts.MaxRetries)

			body := map[string]interface{}{
				"url":          videoURL,
				"downloadMode": "auto",
				"filenameStyle": "basic",
				"videoQuality": "1080",
			}
			if isAudio {
				body["downloadMode"] = "audio"
			}

			data, err := cobaltPost(ctx, apiURL, body)
			if err != nil {
				log.Printf("[Cobalt] [%s] %s failed: %s", jobID, apiURL, err)
				lastErr = err
				if err.Error() == "Cancelled" {
					return nil, err
				}
				continue
			}

			var downloadURL string
			status, _ := data["status"].(string)
			if status == "tunnel" || status == "redirect" {
				downloadURL, _ = data["url"].(string)
			} else if status == "picker" {
				if picker, ok := data["picker"].([]interface{}); ok && len(picker) > 0 {
					if first, ok := picker[0].(map[string]interface{}); ok {
						downloadURL, _ = first["url"].(string)
					}
				}
			}

			if downloadURL == "" {
				lastErr = fmt.Errorf("No download URL from Cobalt")
				continue
			}

			ext := "mp4"
			if isAudio {
				ext = "mp3"
			}
			outputPath := filepath.Join(opts.OutputDir, fmt.Sprintf("%s-cobalt.%s", jobID, ext))
			partPath := outputPath + ".part"

			var startByte int64
			if info, err := os.Stat(partPath); err == nil {
				startByte = info.Size()
				log.Printf("[Cobalt] [%s] Resuming from byte %d", jobID, startByte)
			}

			req, _ := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
			if startByte > 0 {
				req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startByte))
			}

			log.Printf("[Cobalt] [%s] Starting file download...", jobID)
			fileResp, err := http.DefaultClient.Do(req)
			if err != nil {
				lastErr = err
				continue
			}

			if fileResp.StatusCode != 200 && fileResp.StatusCode != 206 {
				fileResp.Body.Close()
				if fileResp.StatusCode == 416 && startByte > 0 {
					os.Rename(partPath, outputPath)
					log.Printf("[Cobalt] [%s] File already complete", jobID)
					return &CobaltDownloadResult{FilePath: outputPath, Ext: ext, DownloadURL: downloadURL}, nil
				}
				lastErr = fmt.Errorf("File download failed: HTTP %d", fileResp.StatusCode)
				continue
			}

			contentLength, _ := strconv.ParseInt(fileResp.Header.Get("Content-Length"), 10, 64)
			totalSize := startByte + contentLength

			flags := os.O_WRONLY | os.O_CREATE
			if startByte > 0 {
				flags |= os.O_APPEND
			} else {
				flags |= os.O_TRUNC
			}
			f, err := os.OpenFile(partPath, flags, 0644)
			if err != nil {
				fileResp.Body.Close()
				lastErr = err
				continue
			}

			downloaded := startByte
			buf := make([]byte, 32*1024)
			var lastProgressLog float64

			for {
				if ctx.Err() != nil {
					f.Close()
					fileResp.Body.Close()
					return nil, fmt.Errorf("Cancelled")
				}

				n, readErr := fileResp.Body.Read(buf)
				if n > 0 {
					f.Write(buf[:n])
					downloaded += int64(n)

					if progressCb != nil && totalSize > 0 {
						progress := math.Min(100, float64(downloaded)/float64(totalSize)*100)
						progressCb(progress, downloaded, totalSize)

						if progress-lastProgressLog >= 25 {
							log.Printf("[Cobalt] [%s] Progress: %.0f%% (%d/%d)", jobID, progress, downloaded, totalSize)
							lastProgressLog = progress
						}
					}
				}
				if readErr != nil {
					if readErr != io.EOF {
						f.Close()
						fileResp.Body.Close()
						lastErr = readErr
						break
					}
					break
				}
			}

			f.Close()
			fileResp.Body.Close()

			if lastErr != nil {
				continue
			}

			os.Rename(partPath, outputPath)

			dur := time.Since(startTime).Seconds()
			info, _ := os.Stat(outputPath)
			log.Printf("[Cobalt] [%s] Success via %s in %.1fs (%.1fMB)", jobID, apiURL, dur, float64(info.Size())/1024/1024)
			return &CobaltDownloadResult{FilePath: outputPath, Ext: ext, DownloadURL: downloadURL}, nil
		}

		if retry < opts.MaxRetries-1 {
			delay := opts.RetryDelay * time.Duration(math.Pow(2, float64(retry)))
			log.Printf("[Cobalt] [%s] All instances failed, retrying in %v...", jobID, delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, fmt.Errorf("Cancelled")
			}
		}
	}

	dur := time.Since(startTime).Seconds()
	log.Printf("[Cobalt] [%s] All %d attempts failed after %.1fs. Last error: %v", jobID, attemptCount, dur, lastErr)
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("All Cobalt instances failed")
}

func StreamClipFromCobalt(ctx context.Context, videoURL, jobID string, startTimeSec, endTimeSec float64, outputPath string, progressCb func(float64)) (*CobaltClipResult, error) {
	log.Printf("[Cobalt] [%s] Getting stream URL for clip trim...", jobID)

	result, err := GetCobaltDownloadURL(ctx, videoURL, false, "")
	if err != nil {
		return nil, err
	}

	duration := endTimeSec - startTimeSec
	log.Printf("[Cobalt] [%s] Trimming %gs to %gs (%gs)", jobID, startTimeSec, endTimeSec, duration)

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-accurate_seek",
		"-ss", fmt.Sprintf("%g", startTimeSec),
		"-i", result.URL,
		"-t", fmt.Sprintf("%g", duration),
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-crf", "18",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		"-y",
		outputPath,
	)

	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	timeRe := ffmpegTimeRe
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			if progressCb != nil {
				if m := timeRe.FindStringSubmatch(scanner.Text()); len(m) > 3 {
					h, _ := strconv.Atoi(m[1])
					min, _ := strconv.Atoi(m[2])
					sec, _ := strconv.Atoi(m[3])
					secs := float64(h*3600 + min*60 + sec)
					progress := math.Min(100, secs/duration*100)
					progressCb(progress)
				}
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("Stream trim failed")
	}

	info, err := os.Stat(outputPath)
	if err != nil || info.Size() < 10000 {
		os.Remove(outputPath)
		return nil, fmt.Errorf("Stream trim produced empty output")
	}

	log.Printf("[Cobalt] [%s] Stream clip complete: %.2fMB", jobID, float64(info.Size())/1024/1024)
	ext := strings.TrimPrefix(filepath.Ext(outputPath), ".")
	if ext == "" {
		ext = "mp4"
	}
	return &CobaltClipResult{FilePath: outputPath, Ext: ext}, nil
}

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
