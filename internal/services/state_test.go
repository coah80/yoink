package services

import (
	"fmt"
	"testing"
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
