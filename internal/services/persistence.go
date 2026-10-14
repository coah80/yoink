package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/coah80/yoink/internal/config"
)

var pendingJobsDir string

func jobsDir() string {
	if pendingJobsDir != "" {
		return pendingJobsDir
	}
	return filepath.Join(config.TempDir, "state", "jobs")
}

func shortJobID(jobID string) string {
	if len(jobID) > 12 {
		return jobID[:12]
	}
	return jobID
}

func pendingJobPath(jobID string) string {
	sum := sha256.Sum256([]byte(jobID))
	return filepath.Join(jobsDir(), hex.EncodeToString(sum[:])+".json")
}

func persistPendingJob(job *PendingJob) error {
	if job == nil || job.ID == "" {
		return fmt.Errorf("pending job has no ID")
	}
	if err := os.MkdirAll(jobsDir(), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(jobsDir(), ".job-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	if err := temp.Chmod(0o600); err != nil {
		cleanup()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, pendingJobPath(job.ID)); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func removePersistedPendingJob(jobID string) error {
	return os.Remove(pendingJobPath(jobID))
}

func (s *State) LoadPendingJobs() (int, error) {
	if err := os.MkdirAll(jobsDir(), 0o700); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(jobsDir())
	if err != nil {
		return 0, err
	}
	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(jobsDir(), entry.Name()))
		if err != nil {
			log.Printf("[Recovery] Failed to read %s: %v", entry.Name(), err)
			continue
		}
		var job PendingJob
		if err := json.Unmarshal(data, &job); err != nil || job.ID == "" || job.Type != "download" {
			log.Printf("[Recovery] Ignoring invalid journal %s", entry.Name())
			continue
		}
		if job.Ready {
			if info, err := os.Stat(job.OutputPath); err != nil || info.Size() == 0 {
				job.Ready = false
				job.OutputPath = ""
				job.OutputExt = ""
				job.Status = "resuming"
			}
		}
		copy := job
		s.muPending.Lock()
		s.pendingJobs[job.ID] = &copy
		s.muPending.Unlock()
		s.muResumed.Lock()
		s.resumedJobs[job.ID] = &ResumedJob{Progress: job.Progress}
		s.muResumed.Unlock()
		loaded++
	}
	return loaded, nil
}

func (s *State) GetPendingJob(jobID string) *PendingJob {
	s.muPending.Lock()
	defer s.muPending.Unlock()
	job := s.pendingJobs[jobID]
	if job == nil {
		return nil
	}
	copy := *job
	return &copy
}

func (s *State) PendingJobs() []*PendingJob {
	s.muPending.Lock()
	jobs := make([]*PendingJob, 0, len(s.pendingJobs))
	for _, job := range s.pendingJobs {
		copy := *job
		jobs = append(jobs, &copy)
	}
	s.muPending.Unlock()
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	return jobs
}

func (s *State) MarkPendingJobReady(jobID, outputPath, outputExt string) error {
	s.muPending.Lock()
	job := s.pendingJobs[jobID]
	if job == nil {
		s.muPending.Unlock()
		return fmt.Errorf("pending job not found")
	}
	job.Ready = true
	job.Status = "ready"
	job.Progress = 100
	job.OutputPath = outputPath
	job.OutputExt = outputExt
	job.UpdatedAt = time.Now().UTC()
	copy := *job
	s.muPending.Unlock()
	return persistPendingJob(&copy)
}

func (s *State) SetShuttingDown(value bool) {
	s.muShutdown.Lock()
	s.shuttingDown = value
	s.muShutdown.Unlock()
}

func (s *State) IsShuttingDown() bool {
	s.muShutdown.RLock()
	defer s.muShutdown.RUnlock()
	return s.shuttingDown
}

func (s *State) SetDraining(value bool) {
	s.muShutdown.Lock()
	s.draining = value
	s.muShutdown.Unlock()
}

func (s *State) IsDraining() bool {
	s.muShutdown.RLock()
	defer s.muShutdown.RUnlock()
	return s.draining
}
