package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSelectVimeoFormatsHonorsQualityAndPrefersStableCDN(t *testing.T) {
	formats := []vimeoFormat{
		{FormatID: "hls-akfire-720", URL: "https://akfire.example/720.m3u8", Vcodec: "avc1", Acodec: "none", Height: 720},
		{FormatID: "hls-fastly-360", URL: "https://fastly.example/360.m3u8", Vcodec: "avc1", Acodec: "none", Height: 360},
		{FormatID: "hls-fastly-720", URL: "https://fastly.example/720.m3u8", Vcodec: "avc1", Acodec: "none", Height: 720},
		{FormatID: "hls-fastly-audio-low", URL: "https://fastly.example/audio-low.m3u8", Vcodec: "none", FormatNote: "English, low"},
		{FormatID: "hls-fastly-audio-high", URL: "https://fastly.example/audio-high.m3u8", Vcodec: "none", FormatNote: "English, high"},
	}

	video, audio, err := selectVimeoFormats(formats, 360, false)
	if err != nil {
		t.Fatal(err)
	}
	if video.FormatID != "hls-fastly-360" {
		t.Fatalf("video = %q, want hls-fastly-360", video.FormatID)
	}
	if audio.FormatID != "hls-fastly-audio-high" {
		t.Fatalf("audio = %q, want hls-fastly-audio-high", audio.FormatID)
	}
}

func TestSelectVimeoFormatsReturnsAudioOnly(t *testing.T) {
	formats := []vimeoFormat{
		{FormatID: "hls-fastly-360", URL: "https://fastly.example/360.m3u8", Vcodec: "avc1", Acodec: "none", Height: 360},
		{FormatID: "hls-fastly-audio-high", URL: "https://fastly.example/audio-high.m3u8", Vcodec: "none", FormatNote: "English, high"},
	}

	video, audio, err := selectVimeoFormats(formats, 360, true)
	if err != nil {
		t.Fatal(err)
	}
	if video.URL != "" {
		t.Fatalf("video URL = %q, want empty", video.URL)
	}
	if audio.FormatID != "hls-fastly-audio-high" {
		t.Fatalf("audio = %q, want hls-fastly-audio-high", audio.FormatID)
	}
}

func TestSelectVimeoFormatsUsesProgressiveCombinedVideo(t *testing.T) {
	formats := []vimeoFormat{
		{FormatID: "http-360p", Protocol: "https", URL: "https://progressive.example/video.mp4", Height: 360},
		{FormatID: "hls-fastly-360", Protocol: "m3u8_native", URL: "https://fastly.example/360.m3u8", Vcodec: "avc1", Acodec: "none", Height: 360},
		{FormatID: "hls-fastly-audio-high", Protocol: "m3u8_native", URL: "https://fastly.example/audio-high.m3u8", Vcodec: "none", FormatNote: "English, high"},
	}

	video, audio, err := selectVimeoFormats(formats, 360, false)
	if err != nil {
		t.Fatal(err)
	}
	if video.FormatID != "http-360p" {
		t.Fatalf("video = %q, want http-360p", video.FormatID)
	}
	if audio.URL != "" {
		t.Fatalf("audio URL = %q, want empty for combined video", audio.URL)
	}
}

func TestValidateVimeoManifestRejectsSampleAES(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,KEYFORMAT=\"com.apple.streamingkeydelivery\"\n"))
	}))
	defer server.Close()

	err := validateVimeoManifest(context.Background(), vimeoFormat{Protocol: "m3u8_native", URL: server.URL})
	if err == nil || !strings.Contains(err.Error(), "DRM") {
		t.Fatalf("error = %v, want DRM rejection", err)
	}
}
