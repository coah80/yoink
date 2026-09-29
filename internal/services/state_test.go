package services

import (
	"fmt"
	"testing"
	"time"
)

func newTestState() *State {
	return &State{
		activeDownloads: make(map[string]*DownloadWriter),
		activeProcesses: make(map[string]*ProcessInfo),
		jobsByType:      make(map[string]int),
		sessions:        make(map[string]*ClientSession),
		jobToClient:     make(map[string]string),
		asyncJobs:       make(map[string]*AsyncJob),
		botDownloads:    make(map[string]*BotDownload),
		pendingJobs:     make(map[string]*PendingJob),
		resumedJobs:     make(map[string]*ResumedJob),
		chunkedUploads:  make(map[string]*ChunkedUpload),
		lastLoggedProg:  make(map[string]float64),
		fileRefs:        make(map[string]*FileRef),
	}
}

func TestTryReserveClientJobCapsPerClient(t *testing.T) {
	state := newTestState()
	clientID := "client-1"

	for i := 0; i < 3; i++ {
		if !state.TryReserveClientJob(fmt.Sprintf("job-%d", i), clientID, 3) {
			t.Fatalf("reservation %d was rejected before the per-client cap", i)
		}
	}

	if state.TryReserveClientJob("job-4", clientID, 3) {
		t.Fatal("fourth reservation was allowed for the same client")
	}

	if count := state.GetClientJobCount(clientID); count != 3 {
		t.Fatalf("client job count = %d, want 3", count)
	}

	if !state.TryReserveClientJob("other-client-job", "client-2", 3) {
		t.Fatal("different client should get its own job budget")
	}

	state.UnlinkJobFromClient("job-0")
	if !state.TryReserveClientJob("job-after-release", clientID, 3) {
		t.Fatal("reservation after release was rejected")
	}
}

func TestKeepTempFile(t *testing.T) {
	s := newTestState()
	now := time.Now()
	s.SetProcess("active-job", &ProcessInfo{JobType: "playlist"})
	s.SetBotDownload("playlist", &BotDownload{FilePath: "/tmp/playlist.zip", CreatedAt: now.Add(-time.Hour), IsWebPlaylist: true})
	s.SetBotDownload("expired", &BotDownload{FilePath: "/tmp/expired.zip", CreatedAt: now.Add(-13 * time.Hour), IsPlaylist: true})
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/tmp/active-job", true},
		{"/tmp/active-job.part", true},
		{"/tmp/active-job-final.mp4", true},
		{"/tmp/active-job2", false},
		{"/tmp/playlist.zip", true},
		{"/tmp/expired.zip", false},
		{"/tmp/orphan.zip", false},
	} {
		if got := s.KeepTempFile(tc.path, now); got != tc.want {
			t.Errorf("KeepTempFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
