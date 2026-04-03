package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var instagramURLRe = regexp.MustCompile(`(?i)instagram\.com/(p|reel|reels|stories)/([A-Za-z0-9_-]+)`)

type InstagramMeta struct {
	Title     string
	Author    string
	Thumbnail string
	MediaType string
	Ext       string
}

type oEmbedResponse struct {
	Title      string `json:"title"`
	AuthorName string `json:"author_name"`
	AuthorURL  string `json:"author_url"`
	Thumbnail  string `json:"thumbnail_url"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	MediaType  string `json:"type"`
}

func IsInstagramURL(rawURL string) bool {
	return instagramURLRe.MatchString(rawURL)
}

func ExtractInstagramShortcode(rawURL string) string {
	m := instagramURLRe.FindStringSubmatch(rawURL)
	if len(m) > 2 {
		return m[2]
	}
	return ""
}

func FetchInstagramMetadata(ctx context.Context, rawURL string) (*InstagramMeta, error) {
	oembedURL := fmt.Sprintf("https://api.instagram.com/oembed?url=%s&omitscript=true", url.QueryEscape(rawURL))

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", oembedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create oembed request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Yoink/1.0)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oembed request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("oembed returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read oembed response: %w", err)
	}

	var oembed oEmbedResponse
	if err := json.Unmarshal(body, &oembed); err != nil {
		return nil, fmt.Errorf("failed to parse oembed response: %w", err)
	}

	mediaType := classifyInstagramMedia(rawURL, oembed.MediaType)
	ext := "mp4"
	if mediaType == "image" {
		ext = "jpg"
	}

	title := oembed.Title
	if title == "" {
		title = fmt.Sprintf("Instagram %s by %s", mediaType, oembed.AuthorName)
	}

	return &InstagramMeta{
		Title:     title,
		Author:    oembed.AuthorName,
		Thumbnail: oembed.Thumbnail,
		MediaType: mediaType,
		Ext:       ext,
	}, nil
}

func DownloadInstagramMedia(ctx context.Context, rawURL, jobID, tempDir string, isAudio bool, progressCb func(float64, int64, int64)) (*DownloadResult, error) {
	log.Printf("[Instagram] [%s] Attempting Cobalt download for: %s", jobID, rawURL)

	cobaltResult, err := DownloadViaCobalt(ctx, rawURL, jobID, isAudio, progressCb, CobaltDownloadOpts{
		OutputDir: tempDir,
	})
	if err == nil {
		log.Printf("[Instagram] [%s] Cobalt download succeeded", jobID)
		return &DownloadResult{
			Path: cobaltResult.FilePath,
			Ext:  cobaltResult.Ext,
		}, nil
	}

	log.Printf("[Instagram] [%s] Cobalt failed, returning error for yt-dlp fallback: %s", jobID, err)
	return nil, fmt.Errorf("cobalt failed for instagram: %w", err)
}

func classifyInstagramMedia(rawURL, oembedType string) string {
	lower := strings.ToLower(rawURL)

	if strings.Contains(lower, "/reel/") || strings.Contains(lower, "/reels/") {
		return "video"
	}

	if strings.Contains(lower, "/stories/") {
		return "video"
	}

	if oembedType == "video" {
		return "video"
	}

	if oembedType == "rich" {
		return "carousel"
	}

	return "image"
}
