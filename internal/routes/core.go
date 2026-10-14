package routes

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
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
	r.Post("/api/admin/drain", handleAdminDrain)
	r.Post("/api/admin/resume", handleAdminResume)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, 200, map[string]interface{}{
		"status":   "ok",
		"version":  config.Version,
		"queue":    services.Global.GetQueueStatus(),
		"draining": services.Global.IsDraining(),
	})
}

func handleAdminDrain(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	services.Global.SetDraining(true)
	respondJSON(w, http.StatusOK, map[string]bool{"draining": true})
}

func handleAdminResume(w http.ResponseWriter, r *http.Request) {
	if !checkAdminAuth(w, r) {
		return
	}
	services.Global.SetDraining(false)
	respondJSON(w, http.StatusOK, map[string]bool{"draining": false})
}

func checkAdminAuth(w http.ResponseWriter, r *http.Request) bool {
	if config.AdminToken == "" {
		http.NotFound(w, r)
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(got), []byte(config.AdminToken)) != 1 {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
		return false
	}
	return true
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
		"maxFileSize":       config.FileSizeLimit,
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

	pendingJob := services.Global.GetPendingJob(id)
	if pendingJob != nil {
		payload := map[string]interface{}{
			"stage":    "resuming",
			"message":  "Reconnected! Resuming download...",
			"progress": pendingJob.Progress,
		}
		if pendingJob.Ready {
			payload["stage"] = "ready"
			payload["message"] = "Recovered download is ready..."
			payload["progress"] = 100
			payload["downloadUrl"] = "/api/recovered/" + pendingJob.ID
			payload["filename"] = pendingJob.Filename
			payload["ext"] = pendingJob.OutputExt
		}
		data, _ := json.Marshal(payload)
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
		processInfo.SignalProcess(syscall.SIGTERM)
		go func() {
			time.Sleep(3 * time.Second)
			if processInfo.CancelFunc != nil {
				processInfo.CancelFunc()
			}
			processInfo.KillProcess()
		}()

		services.Global.ReleaseJob(id)
		services.Global.SendProgressSimple(id, "cancelled", "Download cancelled")

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
