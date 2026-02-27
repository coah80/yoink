package routes

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
	"github.com/coah80/yoink/internal/util"
)

func CoreRoutes(r chi.Router) {
	r.Get("/health", handleHealth)
	r.Post("/api/connect", handleConnect)
	r.Post("/api/heartbeat/{clientId}", handleHeartbeat)
	r.Get("/api/queue-status", handleQueueStatus)
	r.Get("/api/limits", handleLimits)
	r.Get("/api/progress/{id}", handleProgress)
	r.Post("/api/cancel/{id}", handleCancel)
	r.Post("/api/finish-early/{id}", handleFinishEarly)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, 200, map[string]interface{}{
		"status":  "ok",
		"version": "1.0.0",
		"queue":   services.Global.GetQueueStatus(),
	})
}

func handleConnect(w http.ResponseWriter, r *http.Request) {
	clientID := uuid.New().String()
	services.Global.RegisterClient(clientID)
	respondJSON(w, 200, map[string]string{"clientId": clientID})
}

func handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if clientID == "" {
		respondJSON(w, 400, map[string]string{"error": "Client ID required"})
		return
	}

	services.Global.RegisterClient(clientID)
	services.Global.UpdateHeartbeat(clientID)

	session := services.Global.GetClientSession(clientID)
	activeJobs := 0
	if session != nil {
		activeJobs = len(session.ActiveJobs)
	}

	respondJSON(w, 200, map[string]interface{}{
		"success":    true,
		"activeJobs": activeJobs,
	})
}

func handleQueueStatus(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, 200, services.Global.GetQueueStatus())
}

func handleLimits(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, 200, map[string]interface{}{
		"limits":            config.JobLimits,
		"maxFileSize":       15 * 1024 * 1024 * 1024,
		"maxPlaylistVideos": config.MaxPlaylistVideos,
		"maxVideoDuration":  config.MaxVideoDuration,
	})
}

func handleProgress(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", 500)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	resumedJob := services.Global.GetResumedJob(id)
	if resumedJob != nil {
		data, _ := json.Marshal(map[string]interface{}{
			"stage":    "resuming",
			"message":  "Reconnected! Resuming download...",
			"progress": resumedJob.Progress,
		})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		resumedJob.ClientReconnected = true
		resumedJob.Response = w
	} else {
		data, _ := json.Marshal(map[string]interface{}{
			"stage":   "connected",
			"message": "Connected to progress stream",
		})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	dw := services.Global.RegisterDownload(id, w, flusher)

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	select {
	case <-r.Context().Done():
	case <-func() chan struct{} {
		done := make(chan struct{})
		go func() {
			for {
				select {
				case <-ticker.C:
					dw.WriteKeepAlive()
				case <-r.Context().Done():
					close(done)
					return
				}
			}
		}()
		return done
	}():
	}

	services.Global.UnregisterDownload(id)
}

func handleCancel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	clientID := r.URL.Query().Get("clientId")
	if owner := services.Global.GetJobOwner(id); owner != "" && owner != clientID {
		respondJSON(w, 403, map[string]interface{}{"success": false, "message": "Not authorized to cancel this job"})
		return
	}

	processInfo := services.Global.GetProcess(id)
	if processInfo != nil {
		log.Printf("[%s] Cancelling download...\n", id)
		processInfo.SetCancelled(true)

		if processInfo.CancelFunc != nil {
			processInfo.CancelFunc()
		}
		processInfo.KillProcess()

		services.Global.DeleteProcess(id)
		services.Global.SendProgressSimple(id, "cancelled", "Download cancelled")

		go func() {
			time.Sleep(time.Second)
			util.CleanupJobFiles(id)
		}()

		respondJSON(w, 200, map[string]interface{}{"success": true, "message": "Download cancelled"})
	} else {
		respondJSON(w, 200, map[string]interface{}{"success": false, "message": "Download not found or already completed"})
	}
}

func handleFinishEarly(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	clientID := r.URL.Query().Get("clientId")
	if owner := services.Global.GetJobOwner(id); owner != "" && owner != clientID {
		respondJSON(w, 403, map[string]interface{}{"success": false, "message": "Not authorized to modify this job"})
		return
	}

	processInfo := services.Global.GetProcess(id)
	if processInfo != nil {
		log.Printf("[%s] Finishing playlist early...\n", id)
		processInfo.SetFinishEarly(true)
		processInfo.KillProcess()

		services.Global.SendProgressSimple(id, "finishing-early", "Finishing early, packaging downloaded videos...")
		respondJSON(w, 200, map[string]interface{}{"success": true, "message": "Finishing early"})
	} else {
		respondJSON(w, 200, map[string]interface{}{"success": false, "message": "Download not found or already completed"})
	}
}
