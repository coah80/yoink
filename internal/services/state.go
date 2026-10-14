package services

import (
	"bytes"
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

func (p *ProcessInfo) StartCmd(cmd *exec.Cmd) error {
	if p == nil {
		return cmd.Start()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancelled {
		return context.Canceled
	}
	p.cmd = cmd
	return cmd.Start()
}

func (p *ProcessInfo) RunCmd(cmd *exec.Cmd) error {
	if err := p.StartCmd(cmd); err != nil {
		return err
	}
	return cmd.Wait()
}

func (p *ProcessInfo) OutputCmd(cmd *exec.Cmd) ([]byte, error) {
	if cmd.Stdout != nil {
		return nil, fmt.Errorf("exec: Stdout already set")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	captureStderr := cmd.Stderr == nil
	if captureStderr {
		cmd.Stderr = &stderr
	}
	err := p.RunCmd(cmd)
	if exitErr, ok := err.(*exec.ExitError); ok && captureStderr {
		exitErr.Stderr = stderr.Bytes()
	}
	return stdout.Bytes(), err
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
		if p.cmd.Cancel != nil {
			_ = p.cmd.Cancel()
		} else {
			_ = p.cmd.Process.Kill()
		}
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
	SkipHeartbeat bool
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
	StartVideo        int           `json:"startVideo,omitempty"`
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
		"startVideo":        j.StartVideo,
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
		"startVideo":      j.StartVideo,
		"videosCompleted": j.VideosCompleted,
		"currentVideo":    j.CurrentVideo,
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
	JobID         string
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
	Progress          float64
	ClientReconnected bool
	Response          http.ResponseWriter
}

type PendingJob struct {
	DownloadPreferences
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	URL          string    `json:"url"`
	ClientID     string    `json:"clientId,omitempty"`
	Status       string    `json:"status"`
	Progress     float64   `json:"progress"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Resumable    bool      `json:"resumable"`
	Filename     string    `json:"filename"`
	Format       string    `json:"format"`
	Quality      string    `json:"quality"`
	Container    string    `json:"container"`
	AudioFormat  string    `json:"audioFormat"`
	AudioBitrate string    `json:"audioBitrate"`
	TwitterGifs  bool      `json:"twitterGifs"`
	Playlist     bool      `json:"playlist"`
	Ready        bool      `json:"ready"`
	OutputPath   string    `json:"outputPath,omitempty"`
	OutputExt    string    `json:"outputExt,omitempty"`
}

type State struct {
	muDownloads     sync.RWMutex
	activeDownloads map[string]*DownloadWriter

	muProcesses     sync.Mutex
	activeProcesses map[string]*ProcessInfo

	muJobs       sync.Mutex
	jobsByType   map[string]int
	queuedByType map[string]int

	muSessions  sync.Mutex
	sessions    map[string]*ClientSession
	jobToClient map[string]string

	muAsync   sync.RWMutex
	asyncJobs map[string]*AsyncJob

	muBot        sync.RWMutex
	botDownloads map[string]*BotDownload

	muPending   sync.Mutex
	pendingJobs map[string]*PendingJob

	muResumed   sync.Mutex
	resumedJobs map[string]*ResumedJob

	muChunked      sync.Mutex
	chunkedUploads map[string]*ChunkedUpload

	muProgress     sync.Mutex
	lastLoggedProg map[string]float64

	muFileRefs sync.Mutex
	fileRefs   map[string]*FileRef

	muShutdown   sync.RWMutex
	shuttingDown bool
	draining     bool
}

type FileRef struct {
	FilePath  string
	FileName  string
	CreatedAt time.Time
}

var Global *State
var diskSpaceAvailableGB = getDiskSpaceGB

func init() {
	Global = newState()
}

func newState() *State {
	return &State{
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
		queuedByType:   make(map[string]int),
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

func (s *State) TrySetProcess(id string, info *ProcessInfo) bool {
	s.muProcesses.Lock()
	defer s.muProcesses.Unlock()
	if _, exists := s.activeProcesses[id]; exists {
		return false
	}
	s.activeProcesses[id] = info
	return true
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
	s.muShutdown.RLock()
	draining := s.draining
	s.muShutdown.RUnlock()
	if draining {
		return JobCheck{false, "Server update in progress, retry shortly"}
	}
	s.muJobs.Lock()
	defer s.muJobs.Unlock()

	limit, exists := config.JobLimits[jobType]
	if exists && s.jobsByType[jobType] >= limit {
		return JobCheck{false, fmt.Sprintf("server is busy, too many %s jobs are running right now (limit: %d)", jobType, limit)}
	}

	availGB := diskSpaceAvailableGB()
	requiredGB := float64(config.DiskSpaceMinGB)
	reserveGB := 0
	switch jobType {
	case "download":
		reserveGB = config.DownloadReserveGB
	case "playlist":
		reserveGB = config.PlaylistReserveGB
	case "fetchUrl":
		reserveGB = config.FetchURLReserveGB
	}
	if reserveGB > 0 {
		requiredGB += float64(s.jobsByType[jobType]+1) * float64(reserveGB)
	}
	if availGB < requiredGB {
		if reserveGB > 0 && s.jobsByType[jobType] > 0 {
			return JobCheck{false, fmt.Sprintf("Too many active %s jobs for available disk (%.1fGB free, need %.1fGB reserved)", jobType, availGB, requiredGB)}
		}
		return JobCheck{false, fmt.Sprintf("Low disk space (%.1fGB free, need %.1fGB)", availGB, requiredGB)}
	}

	s.jobsByType[jobType]++
	return JobCheck{true, ""}
}

func (s *State) WaitForJobSlot(ctx context.Context, jobType string) JobCheck {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	queued := false
	defer func() {
		if queued {
			s.muJobs.Lock()
			s.queuedByType[jobType]--
			s.muJobs.Unlock()
		}
	}()

	for {
		if ctx.Err() != nil {
			return JobCheck{false, "Request cancelled while queued"}
		}

		check := s.CanStartJob(jobType)
		if check.OK {
			if ctx.Err() != nil {
				s.DecrementJob(jobType)
				return JobCheck{false, "Request cancelled while queued"}
			}
			return check
		}
		if !strings.Contains(strings.ToLower(check.Reason), fmt.Sprintf("too many %s jobs", jobType)) {
			return check
		}

		if !queued {
			s.muJobs.Lock()
			s.queuedByType[jobType]++
			s.muJobs.Unlock()
			queued = true
		}

		select {
		case <-ctx.Done():
			return JobCheck{false, "Request cancelled while queued"}
		case <-ticker.C:
		}
	}
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
	queued := 0
	for k, v := range s.jobsByType {
		active[k] = v
	}
	for _, v := range s.queuedByType {
		queued += v
	}
	s.muJobs.Unlock()

	return map[string]interface{}{
		"active":      active,
		"queued":      queued,
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
			SkipHeartbeat: strings.HasPrefix(clientID, "ip:"),
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
	now := time.Now()
	session.LastHeartbeat = now
	session.LastActivity = now
	return true
}

func (s *State) ClientHeartbeatFresh(clientID string, now time.Time) bool {
	if clientID == "" {
		return false
	}
	s.muSessions.Lock()
	defer s.muSessions.Unlock()
	session, exists := s.sessions[clientID]
	return exists && now.Sub(session.LastHeartbeat) <= config.HeartbeatTimeout
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

func (s *State) TryReserveClientJob(jobID, clientID string, maxJobs int) bool {
	s.muSessions.Lock()
	defer s.muSessions.Unlock()
	if clientID == "" {
		return false
	}
	session, exists := s.sessions[clientID]
	if !exists {
		session = &ClientSession{
			LastHeartbeat: time.Now(),
			LastActivity:  time.Now(),
			ActiveJobs:    make(map[string]bool),
			SkipHeartbeat: strings.HasPrefix(clientID, "ip:"),
		}
		s.sessions[clientID] = session
		short := clientID
		if len(short) > 8 {
			short = short[:8]
		}
		log.Printf("[Session] Client %s... connected", short)
	}
	if len(session.ActiveJobs) >= maxJobs {
		return false
	}
	session.ActiveJobs[jobID] = true
	session.LastActivity = time.Now()
	s.jobToClient[jobID] = clientID
	return true
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

// KeepTempFile prevents the general 20-minute cleanup from deleting active
// jobs or files still promised by an unexpired download link.
func (s *State) KeepTempFile(path string, now time.Time) bool {
	name := filepath.Base(path)
	s.muProcesses.Lock()
	for id := range s.activeProcesses {
		if name == id || strings.HasPrefix(name, id+"-") || strings.HasPrefix(name, id+".") {
			s.muProcesses.Unlock()
			return true
		}
	}
	s.muProcesses.Unlock()

	s.muBot.RLock()
	defer s.muBot.RUnlock()
	for _, dl := range s.botDownloads {
		if filepath.Clean(dl.FilePath) != filepath.Clean(path) {
			continue
		}
		expiry := config.BotDownloadExpiry
		if dl.IsWebPlaylist || dl.IsPlaylist {
			expiry = config.PlaylistDownloadExp
		}
		if now.Sub(dl.CreatedAt) < expiry {
			return true
		}
	}
	return false
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
	now := time.Now().UTC()
	job.ID = jobID
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	job.Resumable = true
	s.pendingJobs[jobID] = job
	copy := *job
	s.muPending.Unlock()
	if err := persistPendingJob(&copy); err != nil {
		log.Printf("[Recovery] Failed to persist job %s: %v", shortJobID(jobID), err)
	}
}

func (s *State) UpdatePendingJob(jobID string, progress float64, status string) {
	var copy *PendingJob
	s.muPending.Lock()
	if job, ok := s.pendingJobs[jobID]; ok {
		job.Progress = progress
		if status != "" {
			job.Status = status
		}
		job.UpdatedAt = time.Now().UTC()
		cloned := *job
		copy = &cloned
	}
	s.muPending.Unlock()
	if copy != nil {
		if err := persistPendingJob(copy); err != nil {
			log.Printf("[Recovery] Failed to update job %s: %v", shortJobID(jobID), err)
		}
	}
}

func (s *State) RemovePendingJob(jobID string) {
	s.muPending.Lock()
	delete(s.pendingJobs, jobID)
	s.muPending.Unlock()
	if err := removePersistedPendingJob(jobID); err != nil && !os.IsNotExist(err) {
		log.Printf("[Recovery] Failed to remove job %s: %v", shortJobID(jobID), err)
	}
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
		}
	} else {
		log.Printf("[%s] %s: %s", short, stage, message)
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
	return s.releaseJob(jobID, true)
}

func (s *State) ReleaseJobKeepingPending(jobID string) bool {
	return s.releaseJob(jobID, false)
}

func (s *State) releaseJob(jobID string, removePending bool) bool {
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
	if removePending {
		s.RemovePendingJob(jobID)
	}
	s.UnlinkJobFromClient(jobID)

	s.muProgress.Lock()
	delete(s.lastLoggedProg, jobID)
	s.muProgress.Unlock()

	return true
}

type sessionCleanupAction struct {
	clientID string
	jobIDs   []string
	idle     bool
}

func (s *State) cleanupTimedOutSessions(now time.Time, cleanupJobFiles func(string)) {
	var actions []sessionCleanupAction

	s.muSessions.Lock()
	for clientID, session := range s.sessions {
		hasActive := len(session.ActiveJobs) > 0

		if hasActive && !session.SkipHeartbeat && now.Sub(session.LastHeartbeat) > config.HeartbeatTimeout {
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
				continue
			}

			s.RemovePendingJob(jobID)

			s.muSessions.Lock()
			delete(s.jobToClient, jobID)
			s.muSessions.Unlock()

			cleanupJobFiles(jobID)
		}
	}
}

func (s *State) StartSessionCleanup(cleanupJobFiles func(string)) {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		for now := range ticker.C {
			s.cleanupTimedOutSessions(now, cleanupJobFiles)
		}
	}()
}

func (s *State) StartCounterReconciliation() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			s.muProcesses.Lock()
			actualCounts := make(map[string]int)
			for _, p := range s.activeProcesses {
				if p.JobType != "" {
					actualCounts[p.JobType]++
				}
			}
			s.muProcesses.Unlock()

			s.muJobs.Lock()
			leaked := false
			for t, count := range s.jobsByType {
				actual := actualCounts[t]
				if count > actual {
					log.Printf("[Queue] Counter leak detected: %s=%d but only %d active processes. Correcting.", t, count, actual)
					s.jobsByType[t] = actual
					leaked = true
				}
			}
			if leaked {
				b, _ := json.Marshal(s.jobsByType)
				log.Printf("[Queue] Counters corrected: %s", string(b))
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
