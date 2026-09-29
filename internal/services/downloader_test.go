package services

import (
	"encoding/json"
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
