package services

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise yt-dlp's actual selector against offline metadata. No network or
// media downloads are involved; install yt-dlp to run these integration cases.
func TestVideoFormatSelection(t *testing.T) {
	ytdlp, err := exec.LookPath("yt-dlp")
	if err != nil {
		t.Skip("yt-dlp is required for format selection integration tests")
	}
	video := func(id, codec, ext string, height int) map[string]interface{} {
		return map[string]interface{}{
			"format_id": id, "url": "https://example.invalid/" + id,
			"vcodec": codec, "acodec": "none", "ext": ext,
			"height": height, "width": height * 16 / 9, "fps": 30,
		}
	}
	audio := func(id, codec, ext string) map[string]interface{} {
		return map[string]interface{}{
			"format_id": id, "url": "https://example.invalid/" + id,
			"vcodec": "none", "acodec": codec, "ext": ext, "abr": 128,
		}
	}
	formats := []map[string]interface{}{
		video("h264-480", "avc1.640028", "mp4", 480),
		video("vp9-720", "vp9", "webm", 720),
		video("vp9-1080", "vp9", "webm", 1080),
		video("av1-1080", "av01.0.08M.08", "mp4", 1080),
		video("vp9-2160", "vp9", "webm", 2160),
		video("vp9-4320", "vp9", "webm", 4320),
		audio("aac", "mp4a.40.2", "m4a"),
		audio("audio-opus", "opus", "webm"),
	}
	combined := video("combined-2160", "avc1.640028", "mp4", 2160)
	combined["acodec"] = "mp4a.40.2"
	cases := []struct {
		name, quality, codec, container, want string
		formats                               []map[string]interface{}
	}{
		{"resolution before h264", "1080p", "h264", "mp4", "vp9-1080+aac", formats},
		{"720p cap", "720p", "h264", "mp4", "vp9-720+aac", formats},
		{"480p cap", "480p", "h264", "mp4", "h264-480+aac", formats},
		{"4k legacy setting", "4k", "h264", "mp4", "vp9-2160+aac", formats},
		{"4k setting", "2160p", "h264", "mp4", "vp9-2160+aac", formats},
		{"uncapped best", "best", "h264", "mp4", "vp9-4320+aac", formats},
		{"prefer av1 at same resolution", "1080p", "av1", "mp4", "av1-1080+audio-opus", formats},
		{"webm compatible streams", "1080p", "h264", "webm", "vp9-1080+audio-opus", formats},
		{"lower source resolution", "1080p", "h264", "mp4", "h264-480+aac", []map[string]interface{}{formats[0], formats[6]}},
		{"combined stream obeys cap", "720p", "h264", "mp4", "NA", []map[string]interface{}{combined}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := json.Marshal(map[string]interface{}{
				"id": "fixture", "title": "fixture", "extractor": "generic",
				"webpage_url": "https://example.invalid/fixture", "formats": tc.formats,
			})
			if err != nil {
				t.Fatal(err)
			}
			infoPath := filepath.Join(t.TempDir(), "info.json")
			if err := os.WriteFile(infoPath, info, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"--ignore-config", "--simulate", "--no-check-formats", "--load-info-json", infoPath, "--print", "%(format_id)s"}
			args = append(args, VideoFormatArgs(tc.quality, tc.codec, tc.container)...)
			out, err := exec.Command(ytdlp, args...).Output()
			if tc.want == "NA" {
				if err == nil {
					t.Fatal("selected a stream above the requested cap")
				}
				return
			}
			if err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					t.Fatalf("yt-dlp: %s", exit.Stderr)
				}
				t.Fatal(err)
			}
			if got := strings.TrimSpace(string(out)); got != tc.want {
				t.Fatalf("selected %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseYtdlpProgressRejectsNumbersAboveOneHundred(t *testing.T) {
	progress := ParseYtdlpProgress("ticktock-clock-gif-25700487%")
	if progress.Percent != 0 {
		t.Fatalf("Percent = %v, want 0", progress.Percent)
	}
}

func TestDownloadViaYtdlpAllowsPortraitQuality(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	capture := filepath.Join(t.TempDir(), "args")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$CAPTURE_PATH"
output=
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-o" ]; then
		shift
		output="$1"
		break
	fi
	shift
done
output=$(printf '%s' "$output" | sed 's/%(ext)s/mp4/')
printf x > "$output"
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_BIN", bin)
	t.Setenv("CAPTURE_PATH", capture)

	_, err := DownloadViaYtdlp(context.Background(), "https://example.com/video", "portrait", DownloadOpts{
		Quality:   "360p",
		Container: "mp4",
		TempDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "[width<=360]") {
		t.Fatalf("yt-dlp args missing portrait width fallback: %q", string(args))
	}
}

func TestDownloadViaYtdlpKeepsDirectGIF(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	capture := filepath.Join(t.TempDir(), "args")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$CAPTURE_PATH"
output=
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-o" ]; then
		shift
		output="$1"
		break
	fi
	shift
done
output=$(printf '%s' "$output" | sed 's/%(ext)s/gif/')
printf 'GIF89a' > "$output"
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_BIN", bin)
	t.Setenv("CAPTURE_PATH", capture)

	result, err := DownloadViaYtdlp(context.Background(), "https://static.klipy.com/ii/example.gif", "klipy", DownloadOpts{
		Quality:   "360p",
		Container: "gif",
		TempDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Ext != "gif" {
		t.Fatalf("Ext = %q", result.Ext)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), "--merge-output-format") {
		t.Fatalf("yt-dlp args try to merge a GIF: %q", string(args))
	}
}

func TestDownloadViaYtdlpUsesFFmpegForTwitchVOD(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	capture := filepath.Join(t.TempDir(), "args")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$CAPTURE_PATH"
output=
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-o" ]; then
		shift
		output="$1"
		break
	fi
	shift
done
output=$(printf '%s' "$output" | sed 's/%(ext)s/mp4/')
printf x > "$output"
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_BIN", bin)
	t.Setenv("CAPTURE_PATH", capture)

	_, err := DownloadViaYtdlp(context.Background(), "https://www.twitch.tv/videos/1000000001", "twitch-vod", DownloadOpts{
		Quality:   "1080p",
		Container: "mp4",
		TempDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--downloader\nm3u8:ffmpeg\n") {
		t.Fatalf("yt-dlp args missing Twitch HLS downloader: %q", string(args))
	}
}

func TestDownloadViaYtdlpForwardsBilibiliHeaders(t *testing.T) {
	previousClient := socialHTTPClient
	socialHTTPClient = &http.Client{Transport: socialRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/x/web-interface/view" {
			return socialResponse(`{"code":0,"data":{"bvid":"BV1xx411c7mD","cid":41324905972,"title":"Public Bilibili video","duration":51,"owner":{"name":"Uploader"}}}`), nil
		}
		return socialResponse(`{"code":0,"data":{"durl":[{"url":"https://upos.example/video.mp4"}]}}`), nil
	})}
	t.Cleanup(func() {
		socialHTTPClient = previousClient
	})

	bin := filepath.Join(t.TempDir(), "yt-dlp")
	capture := filepath.Join(t.TempDir(), "args")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$CAPTURE_PATH"
output=
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-o" ]; then
		shift
		output="$1"
		break
	fi
	shift
done
output=$(printf '%s' "$output" | sed 's/%(ext)s/mp4/')
printf x > "$output"
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_BIN", bin)
	t.Setenv("CAPTURE_PATH", capture)

	_, err := DownloadViaYtdlp(context.Background(), "https://www.bilibili.com/video/BV1xx411c7mD", "bilibili", DownloadOpts{
		Quality:   "360p",
		Container: "mp4",
		TempDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	argumentText := string(args)
	if !strings.Contains(argumentText, "--referer\nhttps://www.bilibili.com/\n") || !strings.Contains(argumentText, "--user-agent\n") {
		t.Fatalf("yt-dlp args missing Bilibili headers: %q", argumentText)
	}
}

func TestDownloadViaYtdlpSelectsWebMCompatibleStreams(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "yt-dlp")
	capture := filepath.Join(t.TempDir(), "args")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$CAPTURE_PATH"
output=
while [ "$#" -gt 0 ]; do
	if [ "$1" = "-o" ]; then
		shift
		output="$1"
		break
	fi
	shift
done
output=$(printf '%s' "$output" | sed 's/%(ext)s/webm/')
printf x > "$output"
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("YTDLP_BIN", bin)
	t.Setenv("CAPTURE_PATH", capture)

	_, err := DownloadViaYtdlp(context.Background(), "https://example.com/video", "webm", DownloadOpts{
		Quality:   "480p",
		Container: "webm",
		TempDir:   t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	argumentText := string(args)
	if !strings.Contains(argumentText, "bv*[ext=webm][height<=480]+ba[ext=webm]") {
		t.Fatalf("yt-dlp args missing WebM-compatible streams: %q", argumentText)
	}
	if strings.Contains(argumentText, "vcodec^=avc") || strings.Contains(argumentText, "acodec^=mp4a") {
		t.Fatalf("yt-dlp args select MP4-only codecs for WebM: %q", argumentText)
	}
}
