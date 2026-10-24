package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
	"github.com/coah80/yoink/internal/util"
)

var (
	allowedStemFormats = []string{"mp3", "m4a", "opus", "flac", "wav"}
	fetchPrefixRe      = regexp.MustCompile(`^fetch-[0-9a-f-]{36}-`)
)

type stemsOpts struct {
	Mode     string `json:"mode"`
	Format   string `json:"format"`
	Bitrate  string `json:"bitrate"`
	ClientID string `json:"clientId"`
}

func (o stemsOpts) validate() error {
	if !config.Contains(services.AllowedStemModes, o.Mode) {
		return fmt.Errorf("Invalid mode. Allowed: %s", strings.Join(services.AllowedStemModes, ", "))
	}
	if !config.Contains(allowedStemFormats, o.Format) {
		return fmt.Errorf("Invalid format. Allowed: %s", strings.Join(allowedStemFormats, ", "))
	}
	if !config.Contains(config.AllowedAudioBitrates, o.Bitrate) {
		return fmt.Errorf("Invalid bitrate. Allowed: %s", strings.Join(config.AllowedAudioBitrates, ", "))
	}
	return nil
}

func StemsRoutes(r chi.Router) {
	r.Post("/api/stems", handleStems)
	r.Post("/api/stems-chunked", handleStemsChunked)
}

func handleStems(w http.ResponseWriter, r *http.Request) {
	filePath, originalName, err := saveUploadedFile(r, w, "file")
	if err != nil {
		respondJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	opts := stemsOpts{
		Mode:     formValueOr(r, "mode", "both"),
		Format:   formValueOr(r, "format", "mp3"),
		Bitrate:  formValueOr(r, "bitrate", "320"),
		ClientID: r.FormValue("clientId"),
	}
	startStemsJob(w, filePath, originalName, opts)
}

func handleStemsChunked(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FilePath string `json:"filePath"`
		FileName string `json:"fileName"`
		stemsOpts
	}
	body.stemsOpts = stemsOpts{Mode: "both", Format: "mp3", Bitrate: "320"}
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
	startStemsJob(w, validPath, orDefault(body.FileName, "audio"), body.stemsOpts)
}

func startStemsJob(w http.ResponseWriter, inputPath, originalName string, opts stemsOpts) {
	reject := func(status int, msg string) {
		os.Remove(inputPath)
		respondJSON(w, status, map[string]string{"error": msg})
	}
	if err := opts.validate(); err != nil {
		reject(400, err.Error())
		return
	}
	if opts.ClientID != "" && services.Global.GetClientJobCount(opts.ClientID) >= config.MaxJobsPerClient {
		reject(429, fmt.Sprintf("Too many active jobs. Maximum %d concurrent jobs per user.", config.MaxJobsPerClient))
		return
	}
	release, err := services.ReserveStemJob()
	if err != nil {
		reject(503, util.ToUserError(err.Error()))
		return
	}
	check := services.Global.CanStartJob("stems")
	if !check.OK {
		release()
		reject(503, check.Reason)
		return
	}

	jobID := uuid.New().String()
	job := &services.AsyncJob{
		Type:      "stems",
		Status:    "processing",
		Message:   "Starting...",
		CreatedAt: time.Now(),
	}
	services.Global.SetAsyncJob(jobID, job)
	ctx, cancel := context.WithCancel(context.Background())
	processInfo := &services.ProcessInfo{JobType: "stems", CancelFunc: cancel}
	services.Global.SetProcess(jobID, processInfo)
	if opts.ClientID != "" {
		services.Global.RegisterClient(opts.ClientID)
		services.Global.LinkJobToClient(jobID, opts.ClientID)
	}
	respondJSON(w, 200, map[string]string{"jobId": jobID})

	go func() {
		defer release()
		defer cancel()
		defer os.Remove(inputPath)
		defer services.Global.ReleaseJob(jobID)
		runStemsJob(ctx, job, processInfo, jobID, inputPath, originalName, opts)
	}()
}

func runStemsJob(ctx context.Context, job *services.AsyncJob, processInfo *services.ProcessInfo, jobID, inputPath, originalName string, opts stemsOpts) {
	if !probeHasStream(inputPath, "a:0", "audio") {
		job.SetError("No audio found in file")
		return
	}
	log.Printf("[%s] Splitting stems | Mode: %s | Format: %s", jobID, opts.Mode, opts.Format)

	name := fetchPrefixRe.ReplaceAllString(filepath.Base(originalName), "")
	base := util.SanitizeFilename(strings.TrimSuffix(name, filepath.Ext(name)))
	out, err := produceStems(ctx, processInfo, jobID, inputPath, config.TempDirs["stems"], base, opts.Mode, services.ProcessVideoOpts{
		IsAudio:      true,
		AudioFormat:  opts.Format,
		AudioBitrate: opts.Bitrate,
		JobID:        jobID,
	}, func(percent float64, message string) {
		job.SetProgressAndMessage(percent*0.97, message)
	})
	if err != nil {
		log.Printf("[%s] Stems failed: %v", jobID, err)
		job.SetError(util.ToUserError(err.Error()))
		return
	}

	job.Lock()
	job.Status = "complete"
	job.Progress = 100
	job.Message = "Stems ready!"
	job.OutputPath = out.Path
	job.OutputFilename = out.Filename + "." + out.Ext
	job.MimeType = out.MimeType
	job.Unlock()
}
