package services

import (
	"fmt"
	"strings"
	"testing"

	"github.com/coah80/yoink/internal/config"
)

func TestDownloadFilenameStyles(t *testing.T) {
	for style, expected := range map[string]string{"basic": "Video Title", "classic": "Video Title (720p)", "pretty": "Video Title - Author (720p, av1)", "nerdy": "Video_Title_720p_av1_opus"} {
		actual := DownloadFilename("Video Title", "720p", DownloadPreferences{Codec: "av1", FilenameStyle: style, Uploader: "Author"})
		if actual != expected {
			t.Fatalf("%s: %q, want %q", style, actual, expected)
		}
	}
}

func TestVideoPreferenceQualityKeepsPortraitSources(t *testing.T) {
	for quality, height := range config.QualityHeight {
		for _, codec := range []string{"h264", "av1", "vp9"} {
			selector := videoPreferenceSelector(DownloadPreferences{Codec: codec}, "mp4", height)
			if !strings.Contains(selector, fmt.Sprintf("[height<=%d]", height)) || !strings.Contains(selector, fmt.Sprintf("[width<=%d]", height)) {
				t.Fatalf("%s/%s omits orientation-aware quality bound: %s", quality, codec, selector)
			}
		}
	}
}

func TestVideoPreferenceSelectorHonorsCodecsAndContainer(t *testing.T) {
	for _, tc := range []struct {
		codec, container, expected string
		hevc                       bool
	}{{"h264", "mp4", "vcodec^=avc", false}, {"av1", "mp4", "vcodec^=av01", false}, {"vp9", "webm", "vcodec^=vp9", false}, {"h264", "webm", "vcodec^=vp9", false}, {"h264", "mp4", "h265", true}} {
		selector := videoPreferenceSelector(DownloadPreferences{Codec: tc.codec, H265: tc.hevc}, tc.container, 720)
		if !strings.Contains(selector, tc.expected) || !strings.Contains(selector, "[height<=720]") {
			t.Fatalf("unexpected selector: %s", selector)
		}
		if !strings.HasSuffix(selector, "/b") {
			t.Fatalf("missing source fallback: %s", selector)
		}
	}
}
