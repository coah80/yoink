package services

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessVideoHonorsCodecAndMetadata(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe unavailable")
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "color=blue:s=128x128:r=5", "-t", "0.4", "-c:v", "libx264", "-metadata", "title=private title", input)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	for _, codec := range []string{"h264", "av1", "vp9"} {
		t.Run(codec, func(t *testing.T) {
			enabled := false
			output := filepath.Join(dir, codec+".mp4")
			result, err := ProcessVideo(input, output, ProcessVideoOpts{DownloadPreferences: DownloadPreferences{Codec: codec, Metadata: &enabled}, Container: "mp4"})
			if err != nil {
				t.Fatal(err)
			}
			if result.Skipped {
				t.Fatal("metadata removal skipped")
			}
			out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_name", "-of", "default=nw=1:nk=1", output).Output()
			if err != nil || strings.TrimSpace(string(out)) != codec {
				t.Fatalf("codec=%s err=%v", out, err)
			}
			out, err = exec.Command("ffprobe", "-v", "error", "-show_entries", "format_tags=title", "-of", "default=nw=1:nk=1", output).Output()
			if err != nil || strings.TrimSpace(string(out)) != "" {
				t.Fatalf("metadata retained: %s err=%v", out, err)
			}
		})
	}
}
