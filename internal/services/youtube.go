package services

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
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
	videoIDRe    = regexp.MustCompile(`"videoId"\s*:\s*"([^"]+)"`)
	startTimeRe  = regexp.MustCompile(`"startTimeMs"\s*:\s*"(\d+)"`)
	endTimeRe    = regexp.MustCompile(`"endTimeMs"\s*:\s*"(\d+)"`)
	clipConfigRe = regexp.MustCompile(`"clipConfig"\s*:\s*\{[^}]*"startTimeMs"\s*:\s*"(\d+)"[^}]*"endTimeMs"\s*:\s*"(\d+)"`)
	// Match videoId that appears near clipConfig (within ~200 chars before or after)
	clipVideoIDRe = regexp.MustCompile(`"clipConfig"\s*:\s*\{[^}]*\}`)
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read clip page: %w", err)
	}
	html := string(body)

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

	// Find videoId closest to clipConfig, not the first one on the page
	var videoID string
	clipConfigLoc := clipVideoIDRe.FindStringIndex(html)
	if clipConfigLoc != nil {
		// Search for videoId within ~500 chars after clipConfig
		searchStart := clipConfigLoc[1]
		searchEnd := searchStart + 500
		if searchEnd > len(html) {
			searchEnd = len(html)
		}
		nearby := html[searchStart:searchEnd]
		if m := videoIDRe.FindStringSubmatch(nearby); len(m) > 1 {
			videoID = m[1]
		}
		// Also check ~500 chars before clipConfig
		if videoID == "" {
			searchStart = clipConfigLoc[0] - 500
			if searchStart < 0 {
				searchStart = 0
			}
			nearby = html[searchStart:clipConfigLoc[0]]
			// Find the LAST videoId before clipConfig (closest one)
			matches := videoIDRe.FindAllStringSubmatch(nearby, -1)
			if len(matches) > 0 {
				videoID = matches[len(matches)-1][1]
			}
		}
	}

	// Fallback: first videoId on page
	if videoID == "" {
		if m := videoIDRe.FindStringSubmatch(html); len(m) > 1 {
			videoID = m[1]
		}
	}

	if videoID == "" {
		return nil, fmt.Errorf("could not find video ID in clip page")
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
