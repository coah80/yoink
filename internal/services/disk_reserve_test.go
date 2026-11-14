package services

import (
	"context"
	"strings"
	"testing"
	"time"
)

func stubDiskSpace(t *testing.T, gb float64) {
	t.Helper()
	original := diskSpaceAvailableGB
	diskSpaceAvailableGB = func() float64 { return gb }
	t.Cleanup(func() { diskSpaceAvailableGB = original })
}

// the reported case: 32.1GB free, one download in flight, flat 15GB reserve each
func TestDownloadShortOnlyBecauseOfOtherJobsQueues(t *testing.T) {
	stubDiskSpace(t, 32.1)
	state := newState()
	first := state.CanStartJob("download")
	if !first.OK {
		t.Fatalf("first download refused: %#v", first)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := make(chan JobCheck, 1)
	go func() { result <- state.WaitForJobSlot(ctx, "download", 0) }()

	deadline := time.Now().Add(time.Second)
	for state.GetQueueStatus()["queued"] != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if queued := state.GetQueueStatus()["queued"]; queued != 1 {
		select {
		case check := <-result:
			t.Fatalf("second download failed instead of queueing: %#v", check)
		default:
		}
		t.Fatalf("queued = %v, want 1", queued)
	}

	state.ReleaseSlot("download", first.ReservedGB)
	select {
	case check := <-result:
		if !check.OK {
			t.Fatalf("queued download not admitted: %#v", check)
		}
	case <-time.After(time.Second):
		t.Fatal("queued download not admitted after the other job finished")
	}
}

func TestDownloadOnFullDiskFailsWithoutQueueing(t *testing.T) {
	stubDiskSpace(t, 12)
	state := newState()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	check := state.WaitForJobSlot(ctx, "download", 0)
	if check.OK || check.Busy || !strings.HasPrefix(check.Reason, "Low disk space") {
		t.Fatalf("check = %#v, want a hard low disk error", check)
	}
}

func TestDownloadTooBigForDiskAloneFailsEvenWithOtherJobs(t *testing.T) {
	stubDiskSpace(t, 32.1)
	state := newState()
	if check := state.CanStartJobWithReserve("download", 2); !check.OK {
		t.Fatalf("small download refused: %#v", check)
	}
	check := state.CanStartJobWithReserve("download", 40)
	if check.OK || check.Busy || !strings.HasPrefix(check.Reason, "Low disk space") {
		t.Fatalf("check = %#v, want a hard low disk error", check)
	}
}

func TestKnownSizeDownloadsShareDiskInsteadOfFlatReserve(t *testing.T) {
	stubDiskSpace(t, 32.1)
	state := newState()
	for i := 0; i < 4; i++ {
		if check := state.CanStartJobWithReserve("download", 4); !check.OK {
			t.Fatalf("download %d refused: %#v", i, check)
		}
	}
	if check := state.CanStartJob("download"); check.OK || !check.Busy {
		t.Fatalf("flat-reserve download with 16GB held = %#v, want queued", check)
	}
}

func TestReleaseReturnsEachJobsOwnReserve(t *testing.T) {
	stubDiskSpace(t, 1000)
	state := newState()
	small := state.CanStartJobWithReserve("download", 2)
	flat := state.CanStartJob("download")
	state.SetProcess("small", &ProcessInfo{JobType: "download", ReservedGB: small.ReservedGB})
	state.SetProcess("flat", &ProcessInfo{JobType: "download"})

	state.ReleaseJob("small")
	if got := state.reservedGB["download"]; got != flat.ReservedGB {
		t.Fatalf("reserved after small release = %v, want %v", got, flat.ReservedGB)
	}
	state.ReleaseJob("flat")
	if got, n := state.reservedGB["download"], state.jobsByType["download"]; got != 0 || n != 0 {
		t.Fatalf("after all releases reserved = %v, active = %d", got, n)
	}
}

func TestReleaseProcessOnlyReleasesItsOwnJob(t *testing.T) {
	stubDiskSpace(t, 1000)
	state := newState()
	state.CanStartJob("download")
	old := &ProcessInfo{JobType: "download"}
	state.SetProcess("job", old)
	state.ReleaseJob("job")

	state.CanStartJob("download")
	retry := &ProcessInfo{JobType: "download"}
	state.SetProcess("job", retry)

	if state.ReleaseProcess("job", old) {
		t.Fatal("stale handler released the retry's slot")
	}
	if n := state.jobsByType["download"]; n != 1 {
		t.Fatalf("active = %d, want 1", n)
	}
	if !state.ReleaseProcess("job", retry) || state.jobsByType["download"] != 0 {
		t.Fatalf("own release failed, active = %d", state.jobsByType["download"])
	}
}

func TestReconcileDropsPhantomSlotsAndTheirReserve(t *testing.T) {
	stubDiskSpace(t, 1000)
	state := newState()
	live := state.CanStartJobWithReserve("download", 3)
	state.SetProcess("live", &ProcessInfo{JobType: "download", ReservedGB: live.ReservedGB})
	state.CanStartJob("download") // never registered, never released

	state.reconcileCounters()
	if n, gb := state.jobsByType["download"], state.reservedGB["download"]; n != 1 || gb != 3 {
		t.Fatalf("after reconcile active = %d reserved = %v, want 1 and 3", n, gb)
	}
}

func TestYtdlpEstimatedBytes(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]interface{}
		want int64
	}{
		{"filesize", map[string]interface{}{"filesize": 3e9}, 3e9},
		{"approx", map[string]interface{}{"filesize_approx": 2e9}, 2e9},
		{"merged formats", map[string]interface{}{"requested_formats": []interface{}{
			map[string]interface{}{"filesize": 1e9},
			map[string]interface{}{"filesize_approx": 1e8},
		}}, 11e8},
		{"merged with unknown part", map[string]interface{}{"requested_formats": []interface{}{
			map[string]interface{}{"filesize": 1e9},
			map[string]interface{}{},
		}}, 0},
		{"unknown", map[string]interface{}{"filesize": nil}, 0},
	}
	for _, tc := range cases {
		if got := YtdlpEstimatedBytes(tc.meta); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestEstimatedDownloadReserveGB(t *testing.T) {
	MetadataCache.Set("https://example.com/big", map[string]interface{}{"filesize": int64(3 << 30)}, time.Minute)
	MetadataCache.Set("https://example.com/tiny", map[string]interface{}{"filesize": int64(1 << 20)}, time.Minute)
	MetadataCache.Set("https://example.com/unknown", map[string]interface{}{"title": "x"}, time.Minute)

	if got := EstimatedDownloadReserveGB("https://example.com/big"); got != 6 {
		t.Errorf("big = %v, want 6", got)
	}
	if got := EstimatedDownloadReserveGB("https://example.com/tiny"); got != 1 {
		t.Errorf("tiny = %v, want the 1GB floor", got)
	}
	if got := EstimatedDownloadReserveGB("https://example.com/unknown"); got != 0 {
		t.Errorf("unknown = %v, want 0", got)
	}
	if got := EstimatedDownloadReserveGB("https://example.com/missing"); got != 0 {
		t.Errorf("missing = %v, want 0", got)
	}
}
