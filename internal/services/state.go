package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/util"
)

type ProcessInfo struct {
	mu          sync.Mutex
	cancelled   bool
	finishEarly bool
	cmd         *exec.Cmd
	CancelFunc  context.CancelFunc
	TempFile    string
	TempDir     string
	JobType     string
}

func (p *ProcessInfo) SetCancelled(v bool) {
	p.mu.Lock()
	p.cancelled = v
	p.mu.Unlock()
}

func (p *ProcessInfo) IsCancelled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cancelled
}

func (p *ProcessInfo) SetFinishEarly(v bool) {
	p.mu.Lock()
	p.finishEarly = v
	p.mu.Unlock()
}

func (p *ProcessInfo) IsFinishEarly() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.finishEarly
}

func (p *ProcessInfo) SetCmd(c *exec.Cmd) {
	p.mu.Lock()
	p.cmd = c
	p.mu.Unlock()
}

func (p *ProcessInfo) GetCmd() *exec.Cmd {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd
}

func (p *ProcessInfo) KillProcess() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
}

func (p *ProcessInfo) SignalProcess(sig os.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Signal(sig)
	}
}

type ClientSession struct {
	LastHeartbeat time.Time
	LastActivity  time.Time
	ActiveJobs    map[string]bool
}

type FailedVideo struct {
	Num    int    `json:"num"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type AsyncJob struct {
	mu                sync.RWMutex
	Status            string        `json:"status"`
	Progress          float64       `json:"progress"`
	Message           string        `json:"message"`
	CreatedAt         time.Time     `json:"-"`
	Type              string        `json:"type,omitempty"`
	URL               string        `json:"url,omitempty"`
	Format            string        `json:"format,omitempty"`
	OutputPath        string        `json:"-"`
	OutputFilename    string        `json:"outputFilename,omitempty"`
	MimeType          string        `json:"-"`
	TextContent       string        `json:"textContent,omitempty"`
	Error             string        `json:"error,omitempty"`
	DownloadToken     string        `json:"downloadToken,omitempty"`
	FileName          string        `json:"fileName,omitempty"`
	FileSize          int64         `json:"fileSize,omitempty"`
	PlaylistTitle     string        `json:"playlistTitle,omitempty"`
	TotalVideos       int           `json:"totalVideos,omitempty"`
	VideosCompleted   int           `json:"videosCompleted,omitempty"`
	CurrentVideo      int           `json:"currentVideo,omitempty"`
	CurrentVideoTitle string        `json:"currentVideoTitle,omitempty"`
	FailedVideos      []FailedVideo `json:"failedVideos,omitempty"`
	FailedCount       int           `json:"failedCount,omitempty"`
	Speed             string        `json:"speed,omitempty"`
	ETA               string        `json:"eta,omitempty"`
	DebugError        string        `json:"debugError,omitempty"`
	PlaylistInfo      interface{}   `json:"playlistInfo,omitempty"`
}

func (j *AsyncJob) SetStatus(status string) {
	j.mu.Lock()
	j.Status = status
	j.mu.Unlock()
}

func (j *AsyncJob) SetProgress(progress float64) {
	j.mu.Lock()
	j.Progress = progress
	j.mu.Unlock()
}

func (j *AsyncJob) SetMessage(message string) {
	j.mu.Lock()
	j.Message = message
	j.mu.Unlock()
}

func (j *AsyncJob) SetProgressAndMessage(progress float64, message string) {
	j.mu.Lock()
	j.Progress = progress
	j.Message = message
	j.mu.Unlock()
}

func (j *AsyncJob) SetError(errMsg string) {
	j.mu.Lock()
	j.Status = "error"
	j.Error = errMsg
	j.mu.Unlock()
}

func (j *AsyncJob) SetComplete(outputPath, outputFilename, mimeType string) {
	j.mu.Lock()
	j.Status = "complete"
	j.Progress = 100
	j.OutputPath = outputPath
	j.OutputFilename = outputFilename
	j.MimeType = mimeType
	j.mu.Unlock()
}

func (j *AsyncJob) SetTextContent(content string) {
	j.mu.Lock()
	j.TextContent = content
	j.mu.Unlock()
}

func (j *AsyncJob) GetStatus() (status string, progress float64, message string, errMsg string, textContent string) {
	j.mu.RLock()
	status = j.Status
	progress = j.Progress
	message = j.Message
	errMsg = j.Error
	textContent = j.TextContent
	j.mu.RUnlock()
	return
}

func (j *AsyncJob) GetDownloadInfo() (outputPath, outputFilename, mimeType, status string) {
	j.mu.RLock()
	outputPath = j.OutputPath
	outputFilename = j.OutputFilename
	mimeType = j.MimeType
	status = j.Status
	j.mu.RUnlock()
	return
}

func (j *AsyncJob) GetPlaylistStatus() map[string]interface{} {
	j.mu.RLock()
	defer j.mu.RUnlock()
	var fv []FailedVideo
	if len(j.FailedVideos) > 0 {
		fv = make([]FailedVideo, len(j.FailedVideos))
		copy(fv, j.FailedVideos)
	}
	return map[string]interface{}{
		"status":            j.Status,
		"progress":          j.Progress,
		"message":           j.Message,
		"playlistTitle":     j.PlaylistTitle,
		"totalVideos":       j.TotalVideos,
		"videosCompleted":   j.VideosCompleted,
		"currentVideo":      j.CurrentVideo,
		"currentVideoTitle": j.CurrentVideoTitle,
		"failedVideos":      fv,
		"failedCount":       j.FailedCount,
		"downloadToken":     j.DownloadToken,
		"fileName":          j.FileName,
		"fileSize":          j.FileSize,
		"speed":             j.Speed,
		"eta":               j.ETA,
	}
}

func (j *AsyncJob) GetBotStatus() map[string]interface{} {
	j.mu.RLock()
	defer j.mu.RUnlock()
	var fv []FailedVideo
	if len(j.FailedVideos) > 0 {
		fv = make([]FailedVideo, len(j.FailedVideos))
		copy(fv, j.FailedVideos)
	}
	return map[string]interface{}{
		"status":          j.Status,
		"progress":        j.Progress,
		"message":         j.Message,
		"error":           j.Error,
		"fileName":        j.FileName,
		"fileSize":        j.FileSize,
		"downloadToken":   j.DownloadToken,
		"speed":           j.Speed,
		"eta":             j.ETA,
		"totalVideos":     j.TotalVideos,
		"videosCompleted": j.VideosCompleted,
		"failedVideos":    fv,
		"playlistInfo":    j.PlaylistInfo,
		"outputFilename":  j.OutputFilename,
	}
}

func (j *AsyncJob) Lock() {
	j.mu.Lock()
}

func (j *AsyncJob) Unlock() {
	j.mu.Unlock()
}

type BotDownload struct {
	FilePath      string
	FileName      string
	FileSize      int64
	MimeType      string
	CreatedAt     time.Time
	Downloaded    bool
	IsWebPlaylist bool
	IsPlaylist    bool
}

type ChunkedUpload struct {
	mu             sync.Mutex
	FileName       string
	FileSize       int64
	TotalChunks    int
	ReceivedChunks map[int]bool
	LastActivity   time.Time
}

func (u *ChunkedUpload) MarkChunkReceived(index int) (received, total int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.ReceivedChunks[index] = true
	u.LastActivity = time.Now()
	return len(u.ReceivedChunks), u.TotalChunks
}

func (u *ChunkedUpload) IsComplete() (complete bool, received, total int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	received = len(u.ReceivedChunks)
	total = u.TotalChunks
	complete = received == total
	return
}

func (u *ChunkedUpload) GetTotalChunks() int {
	return u.TotalChunks
}

type DownloadWriter struct {
	mu      sync.Mutex
	W       http.ResponseWriter
	Flusher http.Flusher
}

func (dw *DownloadWriter) Write(data []byte) {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	fmt.Fprintf(dw.W, "data: %s\n\n", data)
	dw.Flusher.Flush()
}

func (dw *DownloadWriter) WriteKeepAlive() {
	dw.mu.Lock()
	defer dw.mu.Unlock()
	fmt.Fprintf(dw.W, ": keepalive\n\n")
	dw.Flusher.Flush()
}

type ResumedJob struct {
	Progress         float64
	ClientReconnected bool
	Response         http.ResponseWriter
}

type PendingJob struct {
	Type      string
	URL       string
	Options   map[string]interface{}
	ClientID  string
	Status    string
	Progress  float64
	CreatedAt time.Time
	Resumable bool
}

type State struct {
	muDownloads    sync.RWMutex
	activeDownloads map[string]*DownloadWriter

	muProcesses    sync.Mutex
	activeProcesses map[string]*ProcessInfo

	muJobs         sync.Mutex
	jobsByType     map[string]int

	muSessions     sync.Mutex
	sessions       map[string]*ClientSession
	jobToClient    map[string]string

	muAsync        sync.RWMutex
	asyncJobs      map[string]*AsyncJob

	muBot          sync.RWMutex
	botDownloads   map[string]*BotDownload

	muPending      sync.Mutex
	pendingJobs    map[string]*PendingJob

	muResumed      sync.Mutex
	resumedJobs    map[string]*ResumedJob

	muChunked      sync.Mutex
	chunkedUploads map[string]*ChunkedUpload

	muProgress     sync.Mutex
	lastLoggedProg map[string]float64

	muFileRefs     sync.Mutex
	fileRefs       map[string]*FileRef
}

type FileRef struct {
	FilePath  string
	FileName  string
	CreatedAt time.Time
}

var Global *State

func init() {
	Global = &State{
		activeDownloads: make(map[string]*DownloadWriter),
		activeProcesses: make(map[string]*ProcessInfo),
		jobsByType: map[string]int{
			"download":   0,
			"playlist":   0,
			"convert":    0,
			"compress":   0,
			"transcribe": 0,
			"fetchUrl":   0,
		},
		sessions:       make(map[string]*ClientSession),
		jobToClient:    make(map[string]string),
		asyncJobs:      make(map[string]*AsyncJob),
		botDownloads:   make(map[string]*BotDownload),
		pendingJobs:    make(map[string]*PendingJob),
		resumedJobs:    make(map[string]*ResumedJob),
		chunkedUploads: make(map[string]*ChunkedUpload),
		lastLoggedProg: make(map[string]float64),
		fileRefs:       make(map[string]*FileRef),
	}
}

func (s *State) SetFileRef(token string, ref *FileRef) {
	s.muFileRefs.Lock()
	s.fileRefs[token] = ref
	s.muFileRefs.Unlock()
}

func (s *State) GetFileRef(token string) *FileRef {
	s.muFileRefs.Lock()
	defer s.muFileRefs.Unlock()
	return s.fileRefs[token]
}

func (s *State) DeleteFileRef(token string) {
	s.muFileRefs.Lock()
	delete(s.fileRefs, token)
	s.muFileRefs.Unlock()
}

func (s *State) RegisterDownload(id string, w http.ResponseWriter, f http.Flusher) *DownloadWriter {
	dw := &DownloadWriter{W: w, Flusher: f}
	s.muDownloads.Lock()
	s.activeDownloads[id] = dw
	s.muDownloads.Unlock()
	return dw
}

func (s *State) UnregisterDownload(id string) {
	s.muDownloads.Lock()
	delete(s.activeDownloads, id)
	s.muDownloads.Unlock()
}

func (s *State) SetProcess(id string, info *ProcessInfo) {
	s.muProcesses.Lock()
	s.activeProcesses[id] = info
	s.muProcesses.Unlock()
}

func (s *State) GetProcess(id string) *ProcessInfo {
	s.muProcesses.Lock()
	defer s.muProcesses.Unlock()
	return s.activeProcesses[id]
}

func (s *State) DeleteProcess(id string) {
	s.muProcesses.Lock()
	delete(s.activeProcesses, id)
	s.muProcesses.Unlock()
}

type JobCheck struct {
	OK     bool
	Reason string
}

func (s *State) CanStartJob(jobType string) JobCheck {
	s.muJobs.Lock()
	defer s.muJobs.Unlock()

	limit, exists := config.JobLimits[jobType]
	if exists && s.jobsByType[jobType] >= limit {
		return JobCheck{false, fmt.Sprintf("Too many active %s jobs (limit: %d)", jobType, limit)}
	}

	availGB := getDiskSpaceGB()
	if availGB < float64(config.DiskSpaceMinGB) {
		return JobCheck{false, fmt.Sprintf("Low disk space (%.1fGB free, need %dGB)", availGB, config.DiskSpaceMinGB)}
	}

	s.jobsByType[jobType]++
	return JobCheck{true, ""}
}

func (s *State) DecrementJob(jobType string) {
	s.muJobs.Lock()
	if s.jobsByType[jobType] > 0 {
		s.jobsByType[jobType]--
	}
	s.muJobs.Unlock()
}

func (s *State) GetQueueStatus() map[string]interface{} {
	s.muJobs.Lock()
	active := make(map[string]int)
	for k, v := range s.jobsByType {
		active[k] = v
	}
	s.muJobs.Unlock()

	return map[string]interface{}{
		"active":      active,
		"queued":      0,
		"limits":      config.JobLimits,
		"diskSpaceGB": getDiskSpaceGB(),
	}
}

func (s *State) GetJobsByType() map[string]int {
	s.muJobs.Lock()
	defer s.muJobs.Unlock()
	cp := make(map[string]int)
	for k, v := range s.jobsByType {
		cp[k] = v
	}
	return cp
}

func (s *State) RegisterClient(clientID string) {
	s.muSessions.Lock()
	defer s.muSessions.Unlock()

	if _, exists := s.sessions[clientID]; !exists {
		s.sessions[clientID] = &ClientSession{
			LastHeartbeat: time.Now(),
			LastActivity:  time.Now(),
			ActiveJobs:    make(map[string]bool),
		}
		short := clientID
		if len(short) > 8 {
			short = short[:8]
		}
		log.Printf("[Session] Client %s... connected", short)
	} else {
		s.sessions[clientID].LastActivity = time.Now()
	}
}

func (s *State) UpdateHeartbeat(clientID string) bool {
	s.muSessions.Lock()
	defer s.muSessions.Unlock()
	session, exists := s.sessions[clientID]
	if !exists {
		return false
	}
	session.LastHeartbeat = time.Now()
	return true
}

func (s *State) GetClientSession(clientID string) *ClientSession {
	s.muSessions.Lock()
	defer s.muSessions.Unlock()
	return s.sessions[clientID]
}

func (s *State) LinkJobToClient(jobID, clientID string) {
	s.muSessions.Lock()
	defer s.muSessions.Unlock()
	session, exists := s.sessions[clientID]
	if !exists || clientID == "" {
		return
	}
	session.ActiveJobs[jobID] = true
	session.LastActivity = time.Now()
	s.jobToClient[jobID] = clientID
}

func (s *State) GetJobOwner(jobID string) string {
	s.muSessions.Lock()
	defer s.muSessions.Unlock()
	return s.jobToClient[jobID]
}

func (s *State) UnlinkJobFromClient(jobID string) {
	s.muSessions.Lock()
	defer s.muSessions.Unlock()
	clientID, exists := s.jobToClient[jobID]
	if !exists {
		return
	}
	if session, ok := s.sessions[clientID]; ok {
		delete(session.ActiveJobs, jobID)
		session.LastActivity = time.Now()
	}
	delete(s.jobToClient, jobID)
}

func (s *State) GetClientJobCount(clientID string) int {
	s.muSessions.Lock()
	defer s.muSessions.Unlock()
	session, exists := s.sessions[clientID]
	if !exists {
		return 0
	}
	return len(session.ActiveJobs)
}

func (s *State) SetAsyncJob(id string, job *AsyncJob) {
	s.muAsync.Lock()
	s.asyncJobs[id] = job
	s.muAsync.Unlock()
}

func (s *State) GetAsyncJob(id string) *AsyncJob {
	s.muAsync.RLock()
	defer s.muAsync.RUnlock()
	return s.asyncJobs[id]
}

func (s *State) DeleteAsyncJob(id string) {
	s.muAsync.Lock()
	delete(s.asyncJobs, id)
	s.muAsync.Unlock()
}

func (s *State) SetBotDownload(token string, dl *BotDownload) {
	s.muBot.Lock()
	s.botDownloads[token] = dl
	s.muBot.Unlock()
}

func (s *State) GetBotDownload(token string) *BotDownload {
	s.muBot.RLock()
	defer s.muBot.RUnlock()
	return s.botDownloads[token]
}

func (s *State) DeleteBotDownload(token string) {
	s.muBot.Lock()
	delete(s.botDownloads, token)
	s.muBot.Unlock()
}

func (s *State) ForEachBotDownload(fn func(token string, dl *BotDownload) bool) {
	s.muBot.Lock()
	defer s.muBot.Unlock()
	for token, dl := range s.botDownloads {
		if fn(token, dl) {
			delete(s.botDownloads, token)
		}
	}
}

func (s *State) RegisterPendingJob(jobID string, job *PendingJob) {
	s.muPending.Lock()
	job.CreatedAt = time.Now()
	job.Resumable = true
	s.pendingJobs[jobID] = job
	s.muPending.Unlock()
}

func (s *State) UpdatePendingJob(jobID string, progress float64, status string) {
	s.muPending.Lock()
	if job, ok := s.pendingJobs[jobID]; ok {
		job.Progress = progress
		if status != "" {
			job.Status = status
		}
	}
	s.muPending.Unlock()
}

func (s *State) RemovePendingJob(jobID string) {
	s.muPending.Lock()
	delete(s.pendingJobs, jobID)
	s.muPending.Unlock()
}

func (s *State) GetResumedJob(id string) *ResumedJob {
	s.muResumed.Lock()
	defer s.muResumed.Unlock()
	return s.resumedJobs[id]
}

func (s *State) DeleteResumedJob(id string) {
	s.muResumed.Lock()
	delete(s.resumedJobs, id)
	s.muResumed.Unlock()
}

func (s *State) SetChunkedUpload(id string, u *ChunkedUpload) {
	s.muChunked.Lock()
	s.chunkedUploads[id] = u
	s.muChunked.Unlock()
}

func (s *State) GetChunkedUpload(id string) *ChunkedUpload {
	s.muChunked.Lock()
	defer s.muChunked.Unlock()
	return s.chunkedUploads[id]
}

func (s *State) DeleteChunkedUpload(id string) {
	s.muChunked.Lock()
	delete(s.chunkedUploads, id)
	s.muChunked.Unlock()
}

func (s *State) CleanupExpiredChunkedUploads() {
	type expiredUpload struct {
		id string
	}
	var expired []expiredUpload

	s.muChunked.Lock()
	now := time.Now()
	for id, u := range s.chunkedUploads {
		u.mu.Lock()
		isExpired := now.Sub(u.LastActivity) > config.ChunkTimeout
		u.mu.Unlock()
		if isExpired {
			expired = append(expired, expiredUpload{id: id})
			delete(s.chunkedUploads, id)
		}
	}
	s.muChunked.Unlock()

	for _, e := range expired {
		short := e.id
		if len(short) > 8 {
			short = short[:8]
		}
		log.Printf("[Chunk] Upload %s timed out, cleaning up", short)
		uploadDir := config.TempDirs["upload"]
		prefix := "chunk-" + e.id + "-"
		entries, _ := os.ReadDir(uploadDir)
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), prefix) {
				os.Remove(filepath.Join(uploadDir, entry.Name()))
			}
		}
	}
}

func (s *State) SendProgress(downloadID, stage, message string, progress *float64, extra map[string]interface{}) {
	s.muDownloads.RLock()
	dw := s.activeDownloads[downloadID]
	s.muDownloads.RUnlock()

	data := map[string]interface{}{
		"stage":   stage,
		"message": message,
	}
	if progress != nil {
		data["progress"] = *progress
	}
	for k, v := range extra {
		data[k] = v
	}
	data["queueStatus"] = s.GetQueueStatus()

	jsonBytes, _ := json.Marshal(data)

	if dw != nil {
		dw.Write(jsonBytes)
	}

	s.muProgress.Lock()
	lastProg := s.lastLoggedProg[downloadID]
	isProgressMsg := progress != nil && *progress >= 0

	short := downloadID
	if len(short) > 8 {
		short = short[:8]
	}

	if isProgressMsg {
		if *progress >= 100 || *progress-lastProg >= 25 {
			log.Printf("[%s] %s: %s", short, stage, message)
			s.lastLoggedProg[downloadID] = *progress
			if *progress >= 100 {
				delete(s.lastLoggedProg, downloadID)
			}
		}
	} else {
		log.Printf("[%s] %s: %s", short, stage, message)
		delete(s.lastLoggedProg, downloadID)
	}
	s.muProgress.Unlock()
}

func (s *State) SendProgressSimple(downloadID, stage, message string) {
	s.SendProgress(downloadID, stage, message, nil, nil)
}

func (s *State) SendProgressWithPercent(downloadID, stage, message string, progress float64) {
	s.SendProgress(downloadID, stage, message, &progress, nil)
}

func (s *State) ReleaseJob(jobID string) bool {
	s.muProcesses.Lock()
	processInfo, exists := s.activeProcesses[jobID]
	if !exists {
		s.muProcesses.Unlock()
		return false
	}
	delete(s.activeProcesses, jobID)
	s.muProcesses.Unlock()

	if processInfo.JobType != "" {
		s.DecrementJob(processInfo.JobType)
	}
	s.RemovePendingJob(jobID)
	s.UnlinkJobFromClient(jobID)
	return true
}

type sessionCleanupAction struct {
	clientID string
	jobIDs   []string
	idle     bool
}

func (s *State) StartSessionCleanup(cleanupJobFiles func(string)) {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for range ticker.C {
			var actions []sessionCleanupAction

			s.muSessions.Lock()
			now := time.Now()
			for clientID, session := range s.sessions {
				hasActive := len(session.ActiveJobs) > 0

				if hasActive && now.Sub(session.LastHeartbeat) > config.HeartbeatTimeout {
					short := clientID
					if len(short) > 8 {
						short = short[:8]
					}
					log.Printf("[Session] Client %s... heartbeat timeout, cancelling %d jobs", short, len(session.ActiveJobs))

					var jobIDs []string
					for jobID := range session.ActiveJobs {
						jobIDs = append(jobIDs, jobID)
					}
					actions = append(actions, sessionCleanupAction{clientID: clientID, jobIDs: jobIDs})
					delete(s.sessions, clientID)

				} else if !hasActive && now.Sub(session.LastActivity) > config.SessionIdleTimeout {
					short := clientID
					if len(short) > 8 {
						short = short[:8]
					}
					log.Printf("[Session] Client %s... idle timeout", short)
					actions = append(actions, sessionCleanupAction{clientID: clientID, idle: true})
					delete(s.sessions, clientID)
				}
			}
			s.muSessions.Unlock()

			for _, action := range actions {
				if action.idle {
					continue
				}
				for _, jobID := range action.jobIDs {
					aj := s.GetAsyncJob(jobID)
					if aj != nil && aj.Type == "playlist" {
						s.muSessions.Lock()
						delete(s.jobToClient, jobID)
						s.muSessions.Unlock()
						continue
					}

					pi := s.GetProcess(jobID)
					if pi != nil {
						pi.SetCancelled(true)
						pi.SignalProcess(syscall.SIGTERM)
						if pi.CancelFunc != nil {
							pi.CancelFunc()
						}
						s.SendProgressSimple(jobID, "cancelled", "Connection lost - task cancelled")
					}

					var jobType string
					s.muProcesses.Lock()
					if procInfo, ok := s.activeProcesses[jobID]; ok {
						jobType = procInfo.JobType
						delete(s.activeProcesses, jobID)
					}
					s.muProcesses.Unlock()
					if jobType != "" {
						s.DecrementJob(jobType)
					}

					s.RemovePendingJob(jobID)

					s.muSessions.Lock()
					delete(s.jobToClient, jobID)
					s.muSessions.Unlock()

					cleanupJobFiles(jobID)
				}
			}
		}
	}()
}

func (s *State) StartCounterReconciliation() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			s.muProcesses.Lock()
			processCount := len(s.activeProcesses)
			s.muProcesses.Unlock()

			if processCount > 0 {
				continue
			}

			s.muJobs.Lock()
			leaked := false
			for t, count := range s.jobsByType {
				if count > 0 {
					log.Printf("[Queue] Counter leak detected: %s=%d with no active processes. Resetting.", t, count)
					s.jobsByType[t] = 0
					leaked = true
				}
			}
			if leaked {
				b, _ := json.Marshal(s.jobsByType)
				log.Printf("[Queue] Counters reset: %s", string(b))
			}
			s.muJobs.Unlock()
		}
	}()
}

func (s *State) StartAsyncJobExpiry() {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		for range ticker.C {
			s.muAsync.Lock()
			now := time.Now()
			for id, job := range s.asyncJobs {
				timeout := config.AsyncJobTimeout
				if job.Type == "playlist" {
					timeout = config.PlaylistDownloadExp
				}
				if now.Sub(job.CreatedAt) > timeout {
					short := id
					if len(short) > 8 {
						short = short[:8]
					}
					status, _, _, _, _ := job.GetStatus()
					log.Printf("[Bot] Job %s... expired (%s)", short, status)
					delete(s.asyncJobs, id)
				}
			}
			s.muAsync.Unlock()
		}
	}()
}

func getDiskSpaceGB() float64 {
	ds, err := util.GetDiskSpace(config.TempDir)
	if err != nil {
		return 999
	}
	return ds.AvailGB
}
