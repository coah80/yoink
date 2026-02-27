package services

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type ClipData struct {
	VideoID      string
	StartTimeMs  int64
	EndTimeMs    int64
	FullVideoURL string
}

var (
	videoIDRe    = regexp.MustCompile(`"videoId"\s*:\s*"([^"]+)"`)
	startTimeRe  = regexp.MustCompile(`"startTimeMs"\s*:\s*"(\d+)"`)
	endTimeRe    = regexp.MustCompile(`"endTimeMs"\s*:\s*"(\d+)"`)
	clipConfigRe = regexp.MustCompile(`"clipConfig"\s*:\s*\{[^}]*"startTimeMs"\s*:\s*"(\d+)"[^}]*"endTimeMs"\s*:\s*"(\d+)"`)
)

func ParseYouTubeClip(ctx context.Context, clipURL string) (*ClipData, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", clipURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch clip page: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read clip page: %w", err)
	}
	html := string(body)

	var videoID string
	if m := videoIDRe.FindStringSubmatch(html); len(m) > 1 {
		videoID = m[1]
	}
	if videoID == "" {
		return nil, fmt.Errorf("could not find video ID in clip page")
	}

	var startMs, endMs int64
	if m := clipConfigRe.FindStringSubmatch(html); len(m) > 2 {
		startMs, _ = strconv.ParseInt(m[1], 10, 64)
		endMs, _ = strconv.ParseInt(m[2], 10, 64)
	}

	if startMs == 0 && endMs == 0 {
		if m := startTimeRe.FindStringSubmatch(html); len(m) > 1 {
			startMs, _ = strconv.ParseInt(m[1], 10, 64)
		}
		if m := endTimeRe.FindStringSubmatch(html); len(m) > 1 {
			endMs, _ = strconv.ParseInt(m[1], 10, 64)
		}
	}

	if startMs == 0 && endMs == 0 {
		return nil, fmt.Errorf("could not find clip timestamps")
	}

	if idx := strings.Index(videoID, "&"); idx >= 0 {
		videoID = videoID[:idx]
	}

	return &ClipData{
		VideoID:      videoID,
		StartTimeMs:  startMs,
		EndTimeMs:    endMs,
		FullVideoURL: fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID),
	}, nil
}
