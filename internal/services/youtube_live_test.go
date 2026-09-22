package services

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveShortsDownload(t *testing.T) {
	if os.Getenv("YOINK_LIVE") == "" {
		t.Skip("set YOINK_LIVE=1 to hit youtube")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	raw := "https://youtube.com/shorts/LLR5A-Y7c0s?is=wueQLx-6RwmPqdLp"
	meta, err := FetchYouTubeMetadata(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Title == "" || meta.ID != "LLR5A-Y7c0s" {
		t.Fatalf("bad meta %#v", meta)
	}
	dir := t.TempDir()
	res, err := DownloadYouTubeVideo(ctx, raw, "shorttest", dir, "720p", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 10000 {
		t.Fatalf("file too small: %d", info.Size())
	}
	t.Logf("title=%s size=%d ext=%s", meta.Title, info.Size(), res.Ext)
}
