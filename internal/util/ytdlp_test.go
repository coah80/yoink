package util

import "testing"

func TestNormalizeYtdlpURLUsesThreadsEmbed(t *testing.T) {
	got := NormalizeYtdlpURL("https://www.threads.net/@example/post/ABCdef12345?x=1")
	want := "https://www.threads.com/@example/post/ABCdef12345/embed"
	if got != want {
		t.Fatalf("NormalizeYtdlpURL() = %q, want %q", got, want)
	}
}

func TestNormalizeYtdlpURLUsesClapperShareHost(t *testing.T) {
	got := NormalizeYtdlpURL("https://www.clapperapp.com/video/MxNDaDboOR2p0b4X?c=sh")
	want := "https://aw.clapperapp.com/video/MxNDaDboOR2p0b4X"
	if got != want {
		t.Fatalf("NormalizeYtdlpURL() = %q, want %q", got, want)
	}
}

func TestNormalizeYtdlpURLLeavesUnmatchedURLsAlone(t *testing.T) {
	for _, rawURL := range []string{
		"https://static.klipy.com/ii/example.gif",
		"https://www.youtube.com/watch?v=BpJZAKy3-EI&t=42",
	} {
		if got := NormalizeYtdlpURL(rawURL); got != rawURL {
			t.Fatalf("NormalizeYtdlpURL() = %q, want %q", got, rawURL)
		}
	}
}

func TestYtdlpArgsForURLYouTubeFetchesPOT(t *testing.T) {
	args := YtdlpArgsForURL("https://www.youtube.com/watch?v=BpJZAKy3-EI")
	found := false
	for i, arg := range args {
		if arg == "--extractor-args" && i+1 < len(args) && args[i+1] == "youtube:fetch_pot=always" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("YtdlpArgsForURL() = %q, want youtube:fetch_pot=always", args)
	}
}
