package routes

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
)

func recoveryParams(job *services.PendingJob) url.Values {
	params := url.Values{
		"url":          {job.URL},
		"format":       {job.Format},
		"filename":     {job.Filename},
		"quality":      {job.Quality},
		"container":    {job.Container},
		"audioFormat":  {job.AudioFormat},
		"audioBitrate": {job.AudioBitrate},
		"progressId":   {job.ID},
		"clientId":     {job.ClientID},
		"twitterGifs":  {fmt.Sprintf("%t", job.TwitterGifs)},
		"playlist":     {fmt.Sprintf("%t", job.Playlist)},
		"recovery":     {"true"},
		"codec":        {job.Codec},
		"h265":         {fmt.Sprintf("%t", job.H265)},
	}
	if job.Metadata != nil {
		params.Set("metadata", fmt.Sprintf("%t", *job.Metadata))
	}
	return params
}

func StartRecoveredDownloadExpiry() {
	ticker := time.NewTicker(config.RecoveredCleanup)
	go func() {
		for now := range ticker.C {
			removed, bytes := cleanupExpiredRecoveredDownloads(now, now.Add(-config.RecoveredRetention))
			if removed > 0 {
				log.Printf("[Recovery] Expired %d completed download(s), reclaimed %.1f MB", removed, float64(bytes)/(1024*1024))
			}
		}
	}()
}

func cleanupExpiredRecoveredDownloads(now, cutoff time.Time) (int, int64) {
	removed := 0
	var bytes int64
	for _, job := range services.Global.PendingJobs() {
		if !job.Ready || job.UpdatedAt.After(cutoff) {
			continue
		}
		if services.Global.ClientHeartbeatFresh(job.ClientID, now) {
			continue
		}
		output, ok := recoveredDownloadPath(job.OutputPath)
		if !ok {
			log.Printf("[Recovery] Refusing to expire job %s with an invalid output path", shortID(job.ID))
			continue
		}
		var size int64
		if info, err := os.Stat(output); err == nil {
			size = info.Size()
		}
		if err := os.Remove(output); err != nil && !os.IsNotExist(err) {
			log.Printf("[Recovery] Failed to expire job %s: %v", shortID(job.ID), err)
			continue
		}
		services.Global.RemovePendingJob(job.ID)
		removed++
		bytes += size
	}
	return removed, bytes
}

func ResumePendingDownloads() {
	jobs := services.Global.PendingJobs()
	if len(jobs) == 0 {
		return
	}
	log.Printf("[Recovery] Loaded %d direct download job(s)", len(jobs))
	limit := recoveryConcurrency(config.JobLimits["download"])
	semaphore := make(chan struct{}, limit)
	for _, job := range jobs {
		if job.Ready {
			log.Printf("[Recovery] Job %s is ready for client retrieval", shortID(job.ID))
			continue
		}
		job := job
		go func() {
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			resumePendingDownload(job)
		}()
	}
}

func recoveryConcurrency(downloadLimit int) int {
	if downloadLimit < 2 {
		return 1
	}
	return downloadLimit / 2
}

func resumePendingDownload(job *services.PendingJob) {
	endpoint := fmt.Sprintf("http://127.0.0.1:%s/api/download?%s", config.Port, recoveryParams(job).Encode())
	client := &http.Client{}
	for attempt := 1; attempt <= 6; attempt++ {
		if services.Global.IsShuttingDown() {
			return
		}
		request, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return
		}
		response, err := client.Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024*1024))
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				log.Printf("[Recovery] Job %s resumed and is ready", shortID(job.ID))
				return
			}
			if response.StatusCode != http.StatusServiceUnavailable && response.StatusCode != http.StatusTooManyRequests {
				log.Printf("[Recovery] Job %s ended with HTTP %d", shortID(job.ID), response.StatusCode)
				return
			}
		}
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	log.Printf("[Recovery] Job %s could not be resumed after retries", shortID(job.ID))
}

func handleRecoveredDownloadStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job := services.Global.GetPendingJob(id)
	if job == nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Recovered download not found"})
		return
	}
	refreshRecoveredClient(job, r)
	if job.Ready {
		if info, err := os.Stat(job.OutputPath); err != nil || info.Size() == 0 {
			services.Global.RemovePendingJob(id)
			respondJSON(w, http.StatusNotFound, map[string]string{"error": "Recovered download expired"})
			return
		}
	}
	response := map[string]interface{}{
		"id":       job.ID,
		"status":   job.Status,
		"progress": job.Progress,
		"ready":    job.Ready,
	}
	if job.Ready {
		response["downloadUrl"] = "/api/recovered/" + job.ID
		response["filename"] = job.Filename
		response["ext"] = job.OutputExt
	}
	respondJSON(w, http.StatusOK, response)
}

func handleRecoveredDownload(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	job := services.Global.GetPendingJob(id)
	if job == nil {
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Recovered download not found"})
		return
	}
	refreshRecoveredClient(job, r)
	if !job.Ready {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "Recovered download is still running"})
		return
	}
	streamReadyPendingDownload(w, r, job)
}

func streamReadyPendingDownload(w http.ResponseWriter, r *http.Request, job *services.PendingJob) {
	output, ok := recoveredDownloadPath(job.OutputPath)
	if !ok {
		respondJSON(w, http.StatusForbidden, map[string]string{"error": "Recovered download path is invalid"})
		return
	}
	if info, err := os.Stat(output); err != nil || info.Size() == 0 {
		services.Global.RemovePendingJob(job.ID)
		respondJSON(w, http.StatusNotFound, map[string]string{"error": "Recovered download file is missing"})
		return
	}
	isAudio := job.Format == "audio"
	isGIF := strings.EqualFold(job.OutputExt, "gif")
	services.StreamFile(w, r, output, orDefault(job.Filename, "download"), job.OutputExt,
		services.GetMimeType(job.OutputExt, isAudio, isGIF), job.ID, job.URL, "download", func() {
			if r.Header.Get("Range") != "" {
				return
			}
			if err := os.Remove(output); err != nil && !os.IsNotExist(err) {
				log.Printf("[Recovery] Failed to remove delivered file for job %s: %v", shortID(job.ID), err)
				return
			}
			services.Global.RemovePendingJob(job.ID)
		})
}

func recoveredDownloadPath(raw string) (string, bool) {
	base, err := filepath.Abs(config.TempDirs["download"])
	if err != nil {
		return "", false
	}
	output, err := filepath.Abs(raw)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(base, output)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "", false
	}
	return output, true
}

func refreshRecoveredClient(job *services.PendingJob, r *http.Request) {
	clientID := strings.TrimSpace(r.URL.Query().Get("clientId"))
	if clientID == "" || clientID != job.ClientID {
		return
	}
	services.Global.RegisterClient(clientID)
	services.Global.UpdateHeartbeat(clientID)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
