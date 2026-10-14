package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
)

func TestTwitchClipUsesYtdlpMetadata(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	script := "#!/bin/sh\nprintf 'Holy shiit\\nmp4\\n3976806965\\nradiatorchains\\n30\\nhttps://example.com/thumb.jpg\\n'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_BIN", bin)

	request := httptest.NewRequest("GET", "/api/metadata?url=https%3A%2F%2Fwww.twitch.tv%2Fradiatorchains%2Fclip%2FBitterTacitGrousePartyTime-Bcm1ns1L2yF_uwex", nil)
	response := httptest.NewRecorder()

	handleMetadata(response, request)

	if response.Code != 200 {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["title"] != "Holy shiit" {
		t.Fatalf("title = %q", metadata["title"])
	}
	if metadata["id"] != "3976806965" {
		t.Fatalf("id = %q", metadata["id"])
	}
	if _, exists := metadata["isClip"]; exists {
		t.Fatalf("Twitch clip was classified as a YouTube clip: %s", response.Body.String())
	}
}

func TestBilibiliMetadataUsesPublicResolver(t *testing.T) {
	previous := resolveSocialMedia
	resolveSocialMedia = func(ctx context.Context, rawURL string, maxHeight int) (*services.ResolvedMedia, bool, error) {
		if rawURL != "https://www.bilibili.com/video/BV1xx411c7mD" {
			t.Fatalf("rawURL = %q", rawURL)
		}
		return &services.ResolvedMedia{
			Title:     "Public Bilibili video",
			Ext:       "mp4",
			ID:        "BV1xx411c7mD",
			Uploader:  "乔乔和夫夫",
			Duration:  51,
			Thumbnail: "https://i0.hdslb.com/example.jpg",
		}, true, nil
	}
	t.Cleanup(func() {
		resolveSocialMedia = previous
	})

	request := httptest.NewRequest("GET", "/api/metadata?url=https%3A%2F%2Fwww.bilibili.com%2Fvideo%2FBV1xx411c7mD", nil)
	response := httptest.NewRecorder()

	handleMetadata(response, request)

	if response.Code != 200 {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["title"] != "Public Bilibili video" || metadata["id"] != "BV1xx411c7mD" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestMetadataPassesHostYtdlpArgs(t *testing.T) {
	for _, tc := range []struct {
		name, url, output, want string
	}{
		{"mastodon", "https%3A%2F%2Fmastodon.social%2F%40someone%2F110000000000000001", "Test post\\nmp4\\n110000000000000001\\nsomeone\\n9\\nhttps://example.com/thumb.jpg\\n", "--force-generic-extractor\n"},
		{"tumblr", "https%3A%2F%2Fexample.tumblr.com%2Fpost%2F710000000000000001%2Ftest-post", "Test post\\nmp4\\n710000000000000001\\nexample\\n12\\nhttps://example.com/thumb.jpg\\n", "--force-generic-extractor\n"},
		{"lemon8", "https%3A%2F%2Fwww.lemon8-app.com%2F%40someone%2F7400000000000000001%3Fregion%3Dus", "Test video\\nmp4\\n7400000000000000001\\nsomeone\\n54\\nhttps://example.com/thumb.jpg\\n", "--force-generic-extractor\n"},
		{"vimeo", "https%3A%2F%2Fvimeo.com%2F100000001", "Test video\\nmp4\\n100000001\\nVimeo\\n183\\nhttps://example.com/thumb.jpg\\n", "https://player.vimeo.com/video/100000001"},
		{"giphy", "https%3A%2F%2Fgiphy.com%2Fgifs%2Ftest-gif-AbCdEf123456", "Trending GIF\\nmp4\\ngiphy\\n\\n\\nhttps://example.com/thumb.jpg\\n", "--add-headers\nAccept:*/*\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "yt-dlp")
			capture := filepath.Join(t.TempDir(), "args")
			script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE_PATH\"\nprintf '" + tc.output + "'\n"
			if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("YTDLP_BIN", bin)
			t.Setenv("CAPTURE_PATH", capture)

			request := httptest.NewRequest("GET", "/api/metadata?url="+tc.url, nil)
			response := httptest.NewRecorder()

			handleMetadata(response, request)

			if response.Code != 200 {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			args, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(args), tc.want) {
				t.Fatalf("yt-dlp args missing %q: %q", tc.want, string(args))
			}
		})
	}
}

func TestMetadataPreservesMultilineTitle(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	script := `#!/bin/sh
case "$*" in
  *--dump-single-json*)
    printf '{"title":"Test title\\nfor a reason","ext":"mp4","id":"7300000000000000001","uploader":"Test Uploader","duration":88,"thumbnail":"https://example.com/thumb.jpg"}\n'
    ;;
  *)
    printf 'Test title\nfor a reason\nmp4\n7300000000000000001\nTest Uploader\n88\nhttps://example.com/thumb.jpg\n'
    ;;
esac
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_BIN", bin)

	request := httptest.NewRequest("GET", "/api/metadata?url=https%3A%2F%2Fwww.linkedin.com%2Fposts%2Fexample", nil)
	response := httptest.NewRecorder()

	handleMetadata(response, request)

	if response.Code != 200 {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["title"] != "Test title\nfor a reason" {
		t.Fatalf("title = %q", metadata["title"])
	}
	if metadata["duration"] != "88" {
		t.Fatalf("duration = %q", metadata["duration"])
	}
}

func TestMetadataReadsLargeYtdlpJSON(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	script := `#!/bin/sh
printf '{"title":"Large YouTube result","ext":"mp4","id":"jNQXAC9IVRw","uploader":"Test","duration":1443,"thumbnail":"https://example.com/thumb.jpg","description":"'
head -c 131072 /dev/zero | tr '\0' x
printf '"}\n'
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_BIN", bin)

	request := httptest.NewRequest("GET", "/api/metadata?url=https%3A%2F%2Fyoutu.be%2FjNQXAC9IVRw", nil)
	response := httptest.NewRecorder()

	handleMetadata(response, request)

	if response.Code != 200 {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["title"] != "Large YouTube result" || metadata["id"] != "jNQXAC9IVRw" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestPlaylistMetadataSkipsSleepPreset(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	capture := filepath.Join(t.TempDir(), "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$CAPTURE_PATH\"\nprintf 'YouTube Mix\\n3\\nFirst song\\n'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_BIN", bin)
	t.Setenv("CAPTURE_PATH", capture)

	request := httptest.NewRequest("GET", "/api/metadata?url=https%3A%2F%2Fwww.youtube.com%2Fwatch%3Fv%3DlTlzDfhPtFA%26list%3DRDlTlzDfhPtFA%26playlist%3Dtrue&playlist=true", nil)
	response := httptest.NewRecorder()

	handleMetadata(response, request)

	if response.Code != 200 {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), "-t\nsleep\n") {
		t.Fatalf("playlist metadata used sleep preset: %q", string(args))
	}
}

func TestDownloadedGifKeepsGifOutput(t *testing.T) {
	if !shouldOutputGIF("https://static.klipy.com/ii/example.gif", "gif", false, true, "") {
		t.Fatal("downloaded GIF was not kept as a GIF")
	}
	if shouldOutputGIF("https://static.klipy.com/ii/example.gif", "gif", true, true, "") {
		t.Fatal("audio download was classified as a GIF")
	}
}

func TestRecoveryParamsPreserveDownloadOptions(t *testing.T) {
	job := &services.PendingJob{
		ID:           "restart-job",
		URL:          "https://youtu.be/jNQXAC9IVRw",
		ClientID:     "restart-client",
		Filename:     "restart proof",
		Format:       "video",
		Quality:      "360p",
		Container:    "mp4",
		AudioFormat:  "mp3",
		AudioBitrate: "320",
		TwitterGifs:  true,
	}
	params := recoveryParams(job)
	if params.Get("progressId") != "restart-job" || params.Get("url") != job.URL || params.Get("quality") != "360p" {
		t.Fatalf("params = %s", params.Encode())
	}
	if params.Get("recovery") != "true" || params.Get("clientId") != job.ClientID {
		t.Fatalf("recovery params = %s", params.Encode())
	}
	for key, want := range map[string]string{
		"format":       "video",
		"filename":     "restart proof",
		"container":    "mp4",
		"audioFormat":  "mp3",
		"audioBitrate": "320",
		"twitterGifs":  "true",
		"playlist":     "false",
		"h265":         "false",
	} {
		if got := params.Get(key); got != want {
			t.Fatalf("%s = %q, want %q (params = %s)", key, got, want, params.Encode())
		}
	}
}

func TestRecoveryConcurrencyLeavesFreshDownloadCapacity(t *testing.T) {
	tests := map[int]int{
		0: 1,
		1: 1,
		2: 1,
		6: 3,
	}
	for limit, expected := range tests {
		if actual := recoveryConcurrency(limit); actual != expected {
			t.Fatalf("recoveryConcurrency(%d) = %d, want %d", limit, actual, expected)
		}
	}
}

func TestDownloadRejectsDuplicateProgressID(t *testing.T) {
	id := "duplicate-download-test"
	services.Global.SetProcess(id, &services.ProcessInfo{})
	t.Cleanup(func() {
		services.Global.ReleaseJob(id)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/download?url=https%3A%2F%2Fyoutu.be%2FjNQXAC9IVRw&progressId="+id, nil)
	response := httptest.NewRecorder()
	handleDownload(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestDownloadRejectsDuplicateProgressIDAfterQueue(t *testing.T) {
	id := "queued-duplicate-download-test"
	downloadDir := config.TempDirs["download"]
	config.TempDirs["download"] = t.TempDir()
	limit := config.JobLimits["download"]
	reserve := config.DownloadReserveGB
	config.DownloadReserveGB = 0
	for i := 0; i < limit; i++ {
		if check := services.Global.CanStartJob("download"); !check.OK {
			t.Fatalf("reserve slot %d: %s", i, check.Reason)
		}
	}
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	script := "#!/bin/sh\noutput=''\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = '-o' ]; then\n    shift\n    output=$1\n  fi\n  shift\ndone\noutput=$(printf '%s' \"$output\" | sed 's/%(ext)s/mp4/')\nprintf 'test media' > \"$output\"\nprintf '100%%\\n'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_BIN", bin)
	t.Cleanup(func() {
		config.TempDirs["download"] = downloadDir
		config.DownloadReserveGB = reserve
		services.Global.ReleaseJob(id)
		services.Global.RemovePendingJob(id)
		for i := 0; i < limit+2; i++ {
			services.Global.DecrementJob("download")
		}
	})

	responses := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() {
			request := httptest.NewRequest(http.MethodGet, "/api/download?url=https%3A%2F%2Fexample.com%2Fvideo&progressId="+id, nil)
			request.Header.Set("Range", "bytes=0-0")
			response := httptest.NewRecorder()
			handleDownload(response, request)
			responses <- response
		}()
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		queue := services.Global.GetQueueStatus()
		if queue["queued"].(int) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("duplicate requests did not both enter the queue")
		}
		time.Sleep(10 * time.Millisecond)
	}
	services.Global.DecrementJob("download")
	services.Global.DecrementJob("download")

	conflicts := 0
	for i := 0; i < 2; i++ {
		select {
		case response := <-responses:
			if response.Code == http.StatusConflict {
				conflicts++
			}
		case <-time.After(5 * time.Second):
			t.Fatal("duplicate request did not finish")
		}
	}
	if conflicts != 1 {
		t.Fatalf("conflicts = %d, want 1", conflicts)
	}
}

func TestDownloadWorkContextSurvivesClientDisconnect(t *testing.T) {
	requestContext, disconnect := context.WithCancel(context.Background())
	downloadContext, cancelDownload := newDownloadWorkContext(requestContext)
	disconnect()

	if err := downloadContext.Err(); err != nil {
		t.Fatalf("download context ended with request: %v", err)
	}
	cancelDownload()
	if err := downloadContext.Err(); err != context.Canceled {
		t.Fatalf("download context cancel = %v", err)
	}
}

func TestCancelRemovesPendingJob(t *testing.T) {
	id := "cancel-removes-pending-test"
	services.Global.SetProcess(id, &services.ProcessInfo{
		JobType: "download",
	})
	services.Global.RegisterPendingJob(id, &services.PendingJob{
		Type: "download",
		URL:  "https://www.redgifs.com/watch/example",
	})
	t.Cleanup(func() {
		services.Global.ReleaseJob(id)
		services.Global.RemovePendingJob(id)
	})

	request := httptest.NewRequest(http.MethodPost, "/api/cancel/"+id, nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", id)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	response := httptest.NewRecorder()
	handleCancel(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if pending := services.Global.GetPendingJob(id); pending != nil {
		t.Fatalf("pending job survived cancel: %#v", pending)
	}
	if process := services.Global.GetProcess(id); process != nil {
		t.Fatalf("process survived cancel: %#v", process)
	}
}

func TestRecoveredRangeKeepsFileAndJournal(t *testing.T) {
	downloadDir := config.TempDirs["download"]
	config.TempDirs["download"] = t.TempDir()
	t.Cleanup(func() {
		config.TempDirs["download"] = downloadDir
	})

	id := "recovered-range-test"
	output := filepath.Join(config.TempDirs["download"], id+".mp4")
	if err := os.WriteFile(output, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	services.Global.RegisterPendingJob(id, &services.PendingJob{
		Type:      "download",
		URL:       "https://youtu.be/jNQXAC9IVRw",
		Filename:  "range test",
		Format:    "video",
		Container: "mp4",
	})
	if err := services.Global.MarkPendingJobReady(id, output, "mp4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		services.Global.RemovePendingJob(id)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/download?url=https%3A%2F%2Fyoutu.be%2FjNQXAC9IVRw&progressId="+id, nil)
	request.Header.Set("Range", "bytes=4-")
	response := httptest.NewRecorder()
	handleDownload(response, request)

	if response.Code != http.StatusPartialContent || response.Body.String() != "456789" {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("recovered file was removed: %v", err)
	}
	if pending := services.Global.GetPendingJob(id); pending == nil || !pending.Ready {
		t.Fatalf("pending job = %#v", pending)
	}
}

func TestRecoveredFullDownloadRemovesFileAndJournal(t *testing.T) {
	downloadDir := config.TempDirs["download"]
	config.TempDirs["download"] = t.TempDir()
	t.Cleanup(func() {
		config.TempDirs["download"] = downloadDir
	})

	id := "recovered-full-test"
	output := filepath.Join(config.TempDirs["download"], id+".mp4")
	if err := os.WriteFile(output, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	services.Global.RegisterPendingJob(id, &services.PendingJob{
		Type:      "download",
		URL:       "https://youtu.be/jNQXAC9IVRw",
		Filename:  "full test",
		Format:    "video",
		Container: "mp4",
	})
	if err := services.Global.MarkPendingJobReady(id, output, "mp4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		services.Global.RemovePendingJob(id)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/recovered/"+id, nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", id)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	response := httptest.NewRecorder()
	handleRecoveredDownload(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "0123456789" {
		t.Fatalf("status = %d, body = %q", response.Code, response.Body.String())
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("recovered file still exists: %v", err)
	}
	if pending := services.Global.GetPendingJob(id); pending != nil {
		t.Fatalf("pending job survived full send: %#v", pending)
	}
}

func TestExpiredRecoveredDownloadIsRemovedWithoutActiveClient(t *testing.T) {
	downloadDir := config.TempDirs["download"]
	config.TempDirs["download"] = t.TempDir()
	t.Cleanup(func() {
		config.TempDirs["download"] = downloadDir
	})

	id := "expired-recovered-test"
	output := filepath.Join(config.TempDirs["download"], id+".mp4")
	if err := os.WriteFile(output, []byte("expired"), 0o600); err != nil {
		t.Fatal(err)
	}
	services.Global.RegisterPendingJob(id, &services.PendingJob{Type: "download"})
	if err := services.Global.MarkPendingJobReady(id, output, "mp4"); err != nil {
		t.Fatal(err)
	}
	job := services.Global.GetPendingJob(id)
	t.Cleanup(func() {
		services.Global.RemovePendingJob(id)
	})

	removed, _ := cleanupExpiredRecoveredDownloads(time.Now(), job.UpdatedAt.Add(time.Second))
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("expired file still exists: %v", err)
	}
	if pending := services.Global.GetPendingJob(id); pending != nil {
		t.Fatalf("expired pending job survived: %#v", pending)
	}
}

func TestExpiredRecoveredDownloadStaysWhileClientIsActive(t *testing.T) {
	downloadDir := config.TempDirs["download"]
	config.TempDirs["download"] = t.TempDir()
	t.Cleanup(func() {
		config.TempDirs["download"] = downloadDir
	})

	id := "active-recovered-test"
	clientID := "active-recovered-client"
	output := filepath.Join(config.TempDirs["download"], id+".mp4")
	if err := os.WriteFile(output, []byte("active"), 0o600); err != nil {
		t.Fatal(err)
	}
	services.Global.RegisterClient(clientID)
	services.Global.UpdateHeartbeat(clientID)
	services.Global.RegisterPendingJob(id, &services.PendingJob{Type: "download", ClientID: clientID})
	if err := services.Global.MarkPendingJobReady(id, output, "mp4"); err != nil {
		t.Fatal(err)
	}
	job := services.Global.GetPendingJob(id)
	t.Cleanup(func() {
		services.Global.RemovePendingJob(id)
	})

	removed, _ := cleanupExpiredRecoveredDownloads(time.Now(), job.UpdatedAt.Add(time.Second))
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("active recovery file was removed: %v", err)
	}
	if pending := services.Global.GetPendingJob(id); pending == nil || !pending.Ready {
		t.Fatalf("active pending job = %#v", pending)
	}
}

func TestRecoveredStatusRemovesMissingJournal(t *testing.T) {
	id := "missing-recovered-test"
	services.Global.RegisterPendingJob(id, &services.PendingJob{
		Type:      "download",
		URL:       "https://youtu.be/jNQXAC9IVRw",
		Filename:  "missing test",
		Format:    "video",
		Container: "mp4",
	})
	if err := services.Global.MarkPendingJobReady(id, filepath.Join(t.TempDir(), "missing.mp4"), "mp4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		services.Global.RemovePendingJob(id)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/recovered/"+id+"/status", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", id)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	response := httptest.NewRecorder()
	handleRecoveredDownloadStatus(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if pending := services.Global.GetPendingJob(id); pending != nil {
		t.Fatalf("pending job was not removed: %#v", pending)
	}
}
