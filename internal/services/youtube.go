package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/coah80/yoink/internal/util"
)

type ClipData struct {
	VideoID      string
	StartTimeMs  int64
	EndTimeMs    int64
	FullVideoURL string
}

var (
	initialDataRe = regexp.MustCompile(`(?:var\s+)?(?:ytInitialData|window\["ytInitialData"\])\s*=\s*`)
	playerDataRe  = regexp.MustCompile(`(?:var\s+)?(?:ytInitialPlayerResponse|window\["ytInitialPlayerResponse"\])\s*=\s*`)
	clipVideoIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
)

func ParseYouTubeClip(ctx context.Context, clipURL string) (*ClipData, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", clipURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	// Use cookies for YouTube auth if available
	client := &http.Client{}
	if cookiePath := util.GetCookiePath(); cookiePath != "" {
		if jar, err := loadCookieJar(cookiePath); err == nil {
			client.Jar = jar
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch clip page: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clip page returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read clip page: %w", err)
	}
	html := string(body)

	var player struct {
		VideoDetails struct {
			VideoID string `json:"videoId"`
		} `json:"videoDetails"`
		ClipConfig struct {
			Start json.Number `json:"startTimeMs"`
			End   json.Number `json:"endTimeMs"`
		} `json:"clipConfig"`
	}
	pos := playerDataRe.FindStringIndex(html)
	if pos == nil {
		return nil, fmt.Errorf("could not find clip player data")
	}
	if err := json.NewDecoder(strings.NewReader(html[pos[1]:])).Decode(&player); err != nil {
		return nil, fmt.Errorf("invalid clip player data: %w", err)
	}
	var initial struct {
		CurrentVideoEndpoint struct {
			WatchEndpoint struct {
				VideoID string `json:"videoId"`
			} `json:"watchEndpoint"`
		} `json:"currentVideoEndpoint"`
	}
	if pos := initialDataRe.FindStringIndex(html); pos != nil {
		if err := json.NewDecoder(strings.NewReader(html[pos[1]:])).Decode(&initial); err != nil {
			return nil, fmt.Errorf("invalid clip endpoint data: %w", err)
		}
	}
	videoID := initial.CurrentVideoEndpoint.WatchEndpoint.VideoID
	if videoID == "" {
		videoID = player.VideoDetails.VideoID
	}
	if !clipVideoIDRe.MatchString(videoID) {
		return nil, fmt.Errorf("could not find clip parent video ID")
	}
	startMs, startErr := player.ClipConfig.Start.Int64()
	endMs, endErr := player.ClipConfig.End.Int64()
	if startErr != nil || endErr != nil || startMs < 0 || endMs <= startMs {
		return nil, fmt.Errorf("invalid clip timestamps")
	}

	return &ClipData{
		VideoID:      videoID,
		StartTimeMs:  startMs,
		EndTimeMs:    endMs,
		FullVideoURL: fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID),
	}, nil
}

// loadCookieJar parses a Netscape cookies.txt file into an http.CookieJar.
func loadCookieJar(path string) (*cookiejar.Jar, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	jar, _ := cookiejar.New(nil)
	scanner := bufio.NewScanner(f)
	var cookies []*http.Cookie

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 7 {
			continue
		}
		domain := parts[0]
		if !strings.Contains(domain, "youtube.com") && !strings.Contains(domain, "google.com") {
			continue
		}
		cookies = append(cookies, &http.Cookie{
			Name:   parts[5],
			Value:  parts[6],
			Domain: domain,
			Path:   parts[2],
			Secure: parts[3] == "TRUE",
		})
	}

	if len(cookies) > 0 {
		u, _ := url.Parse("https://www.youtube.com")
		jar.SetCookies(u, cookies)
	}
	return jar, nil
}
