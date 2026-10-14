package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coah80/yoink/internal/config"
)

func TestPendingJobsSurviveStateReload(t *testing.T) {
	originalDir := pendingJobsDir
	pendingJobsDir = filepath.Join(t.TempDir(), "jobs")
	t.Cleanup(func() {
		pendingJobsDir = originalDir
	})

	state := newState()
	state.RegisterPendingJob("restart-job", &PendingJob{
		Type:         "download",
		URL:          "https://youtu.be/jNQXAC9IVRw",
		Status:       "downloading",
		Progress:     37,
		Filename:     "restart proof",
		Format:       "video",
		Quality:      "360p",
		Container:    "mp4",
		AudioFormat:  "mp3",
		AudioBitrate: "320",
		TwitterGifs:  true,
	})

	reloaded := newState()
	count, err := reloaded.LoadPendingJobs()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("loaded jobs = %d, want 1", count)
	}
	job := reloaded.GetPendingJob("restart-job")
	if job == nil || job.URL != "https://youtu.be/jNQXAC9IVRw" || job.Progress != 37 || job.Quality != "360p" {
		t.Fatalf("reloaded job = %#v", job)
	}

	output := filepath.Join(t.TempDir(), "restart-job-final.mp4")
	if err := os.WriteFile(output, []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.MarkPendingJobReady("restart-job", output, "mp4"); err != nil {
		t.Fatal(err)
	}
	ready := reloaded.GetPendingJob("restart-job")
	if ready == nil || !ready.Ready || ready.OutputPath != output || ready.OutputExt != "mp4" {
		t.Fatalf("ready job = %#v", ready)
	}

	reloaded.RemovePendingJob("restart-job")
	entries, err := os.ReadDir(pendingJobsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("journal entries remain: %v", entries)
	}
}

func TestDrainingStateRejectsNewJobs(t *testing.T) {
	state := newState()
	state.SetDraining(true)
	check := state.CanStartJob("download")
	if check.OK || check.Reason != "Server update in progress, retry shortly" {
		t.Fatalf("check = %#v", check)
	}
	state.SetDraining(false)
	if check := state.CanStartJob("download"); !check.OK {
		t.Fatalf("job was not allowed after drain ended: %#v", check)
	}
}

func TestWaitForJobSlotQueuesUntilCapacityFrees(t *testing.T) {
	previousLimit := config.JobLimits["download"]
	config.JobLimits["download"] = 1
	t.Cleanup(func() {
		config.JobLimits["download"] = previousLimit
	})

	state := newState()
	if check := state.CanStartJob("download"); !check.OK {
		t.Fatalf("first job was not allowed: %#v", check)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan JobCheck, 1)
	go func() {
		result <- state.WaitForJobSlot(ctx, "download")
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for state.GetQueueStatus()["queued"] != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if queued := state.GetQueueStatus()["queued"]; queued != 1 {
		t.Fatalf("queued = %v, want 1", queued)
	}

	state.DecrementJob("download")
	select {
	case check := <-result:
		if !check.OK {
			t.Fatalf("queued job was not admitted: %#v", check)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("queued job was not admitted after capacity freed")
	}

	status := state.GetQueueStatus()
	if queued := status["queued"]; queued != 0 {
		t.Fatalf("queued = %v, want 0", queued)
	}
	active := status["active"].(map[string]int)
	if active["download"] != 1 {
		t.Fatalf("active downloads = %d, want 1", active["download"])
	}
}

func TestWaitForJobSlotRemovesCancelledWaiter(t *testing.T) {
	previousLimit := config.JobLimits["download"]
	config.JobLimits["download"] = 1
	t.Cleanup(func() {
		config.JobLimits["download"] = previousLimit
	})

	state := newState()
	if check := state.CanStartJob("download"); !check.OK {
		t.Fatalf("first job was not allowed: %#v", check)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan JobCheck, 1)
	go func() {
		result <- state.WaitForJobSlot(ctx, "download")
	}()

	deadline := time.Now().Add(500 * time.Millisecond)
	for state.GetQueueStatus()["queued"] != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case check := <-result:
		if check.OK || check.Reason != "Request cancelled while queued" {
			t.Fatalf("cancelled check = %#v", check)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancelled waiter did not return")
	}
	if queued := state.GetQueueStatus()["queued"]; queued != 0 {
		t.Fatalf("queued = %v, want 0", queued)
	}
}

func TestCanStartDownloadReservesMergeHeadroom(t *testing.T) {
	originalDiskSpace := diskSpaceAvailableGB
	diskSpaceAvailableGB = func() float64 { return 34 }
	t.Cleanup(func() {
		diskSpaceAvailableGB = originalDiskSpace
	})

	state := newState()
	if check := state.CanStartJob("download"); !check.OK {
		t.Fatalf("first download was not allowed: %#v", check)
	}
	if check := state.CanStartJob("download"); check.OK {
		t.Fatalf("second download overcommitted merge space: %#v", check)
	}
}

func TestCanStartPlaylistReservesPackagingHeadroom(t *testing.T) {
	originalDiskSpace := diskSpaceAvailableGB
	diskSpaceAvailableGB = func() float64 { return 34 }
	t.Cleanup(func() {
		diskSpaceAvailableGB = originalDiskSpace
	})

	state := newState()
	if check := state.CanStartJob("playlist"); !check.OK {
		t.Fatalf("first playlist was not allowed: %#v", check)
	}
	if check := state.CanStartJob("playlist"); check.OK {
		t.Fatalf("second playlist overcommitted packaging space: %#v", check)
	}
}

func TestCanStartFetchURLReservesFileHeadroom(t *testing.T) {
	originalDiskSpace := diskSpaceAvailableGB
	diskSpaceAvailableGB = func() float64 { return 20 }
	t.Cleanup(func() {
		diskSpaceAvailableGB = originalDiskSpace
	})

	state := newState()
	if check := state.CanStartJob("fetchUrl"); !check.OK {
		t.Fatalf("first URL fetch was not allowed: %#v", check)
	}
	if check := state.CanStartJob("fetchUrl"); check.OK {
		t.Fatalf("second URL fetch overcommitted file space: %#v", check)
	}
}
