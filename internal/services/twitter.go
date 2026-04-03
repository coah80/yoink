package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var tweetIDRe = regexp.MustCompile(`/status/(\d+)`)

type TwitterMeta struct {
	Title     string
	Author    string
	Duration  float64
	Thumbnail string
	MediaType string
	Ext       string
}

type fxTwitterResponse struct {
	Code  int     `json:"code"`
	Tweet fxTweet `json:"tweet"`
}

type fxTweet struct {
	Text   string         `json:"text"`
	Author fxTweetAuthor  `json:"author"`
	Media  *fxTweetMedia  `json:"media"`
}

type fxTweetAuthor struct {
	Name       string `json:"name"`
	ScreenName string `json:"screen_name"`
	AvatarURL  string `json:"avatar_url"`
}

type fxTweetMedia struct {
	Videos []fxTweetVideo `json:"videos"`
	Photos []fxTweetPhoto `json:"photos"`
	Mosaic *fxTweetMosaic `json:"mosaic"`
}

type fxTweetVideo struct {
	URL          string  `json:"url"`
	ThumbnailURL string  `json:"thumbnail_url"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	Duration     float64 `json:"duration"`
	Type         string  `json:"type"`
}

type fxTweetPhoto struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Type   string `json:"type"`
}

type fxTweetMosaic struct {
	Type    string          `json:"type"`
	Formats fxMosaicFormats `json:"formats"`
}

type fxMosaicFormats struct {
	JPEG string `json:"jpeg"`
	WebP string `json:"webp"`
}

func IsTwitterURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	isTwitterDomain := strings.Contains(lower, "twitter.com/") || strings.Contains(lower, "x.com/")
	return isTwitterDomain && strings.Contains(lower, "/status/")
}

func ExtractTweetID(rawURL string) string {
	m := tweetIDRe.FindStringSubmatch(rawURL)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func fetchFxTwitter(ctx context.Context, tweetID string) (*fxTwitterResponse, error) {
	apiURL := fmt.Sprintf("https://api.fxtwitter.com/status/%s", tweetID)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "yoink/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fxtwitter request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read fxtwitter response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("fxtwitter returned HTTP %d", resp.StatusCode)
	}

	var result fxTwitterResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse fxtwitter response: %w", err)
	}

	if result.Code != 200 {
		return nil, fmt.Errorf("fxtwitter error code %d", result.Code)
	}

	return &result, nil
}

func FetchTwitterMetadata(ctx context.Context, rawURL string) (*TwitterMeta, error) {
	tweetID := ExtractTweetID(rawURL)
	if tweetID == "" {
		return nil, fmt.Errorf("could not extract tweet ID from URL")
	}

	data, err := fetchFxTwitter(ctx, tweetID)
	if err != nil {
		return nil, err
	}

	tweet := data.Tweet
	author := tweet.Author.Name
	if tweet.Author.ScreenName != "" {
		author = fmt.Sprintf("@%s", tweet.Author.ScreenName)
	}

	title := tweet.Text
	if len(title) > 100 {
		title = title[:100] + "..."
	}
	if title == "" {
		title = fmt.Sprintf("Tweet by %s", author)
	}

	meta := &TwitterMeta{
		Title:  title,
		Author: author,
	}

	if tweet.Media == nil {
		meta.MediaType = "image"
		meta.Ext = "txt"
		return meta, nil
	}

	if len(tweet.Media.Videos) > 0 {
		vid := tweet.Media.Videos[0]
		meta.Duration = vid.Duration
		meta.Thumbnail = vid.ThumbnailURL
		meta.Ext = "mp4"
		if vid.Type == "gif" {
			meta.MediaType = "gif"
		} else {
			meta.MediaType = "video"
		}
		return meta, nil
	}

	if len(tweet.Media.Photos) > 0 {
		meta.MediaType = "image"
		meta.Thumbnail = tweet.Media.Photos[0].URL
		meta.Ext = extensionFromPhotoURL(tweet.Media.Photos[0].URL)
		return meta, nil
	}

	if tweet.Media.Mosaic != nil {
		meta.MediaType = "image"
		meta.Thumbnail = tweet.Media.Mosaic.Formats.JPEG
		meta.Ext = "jpg"
		return meta, nil
	}

	return meta, nil
}

func DownloadTwitterMedia(ctx context.Context, rawURL, jobID, tempDir string, isAudio bool, twitterGifs bool, progressCb func(float64, int64, int64)) (*DownloadResult, error) {
	tweetID := ExtractTweetID(rawURL)
	if tweetID == "" {
		return nil, fmt.Errorf("could not extract tweet ID from URL")
	}

	log.Printf("[Twitter] [%s] Fetching tweet %s via fxtwitter", jobID, tweetID)

	data, err := fetchFxTwitter(ctx, tweetID)
	if err != nil {
		return nil, fmt.Errorf("fxtwitter fetch failed: %w", err)
	}

	tweet := data.Tweet
	if tweet.Media == nil {
		return nil, fmt.Errorf("tweet has no media")
	}

	var mediaURL, ext string

	if len(tweet.Media.Videos) > 0 {
		best := selectBestVideo(tweet.Media.Videos)
		mediaURL = best.URL
		ext = "mp4"
		log.Printf("[Twitter] [%s] Video: %dx%d, duration=%.1fs", jobID, best.Width, best.Height, best.Duration)
	} else if len(tweet.Media.Photos) > 0 {
		photo := tweet.Media.Photos[0]
		mediaURL = photo.URL
		ext = extensionFromPhotoURL(photo.URL)
		log.Printf("[Twitter] [%s] Photo: %dx%d", jobID, photo.Width, photo.Height)
	} else if tweet.Media.Mosaic != nil {
		mediaURL = tweet.Media.Mosaic.Formats.JPEG
		ext = "jpg"
		if tweet.Media.Mosaic.Formats.WebP != "" {
			mediaURL = tweet.Media.Mosaic.Formats.WebP
			ext = "webp"
		}
		log.Printf("[Twitter] [%s] Mosaic image", jobID)
	} else {
		return nil, fmt.Errorf("no downloadable media in tweet")
	}

	if mediaURL == "" {
		return nil, fmt.Errorf("empty media URL from fxtwitter")
	}

	outputPath := filepath.Join(tempDir, fmt.Sprintf("%s-twitter.%s", jobID, ext))

	req, err := http.NewRequestWithContext(ctx, "GET", mediaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("media download failed: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	totalSize := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	var lastProgressLog float64

	for {
		if ctx.Err() != nil {
			os.Remove(outputPath)
			return nil, fmt.Errorf("cancelled")
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			f.Write(buf[:n])
			downloaded += int64(n)

			if progressCb != nil && totalSize > 0 {
				progress := math.Min(100, float64(downloaded)/float64(totalSize)*100)
				progressCb(progress, downloaded, totalSize)

				if progress-lastProgressLog >= 25 {
					log.Printf("[Twitter] [%s] Progress: %.0f%% (%d/%d)", jobID, progress, downloaded, totalSize)
					lastProgressLog = progress
				}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				os.Remove(outputPath)
				return nil, fmt.Errorf("download interrupted: %w", readErr)
			}
			break
		}
	}

	info, err := os.Stat(outputPath)
	if err != nil || info.Size() < 100 {
		os.Remove(outputPath)
		return nil, fmt.Errorf("downloaded file too small or missing")
	}

	log.Printf("[Twitter] [%s] Downloaded: %.2fMB (%s)", jobID, float64(info.Size())/1024/1024, ext)
	return &DownloadResult{Path: outputPath, Ext: ext}, nil
}

func selectBestVideo(videos []fxTweetVideo) fxTweetVideo {
	best := videos[0]
	bestPixels := best.Width * best.Height
	for _, v := range videos[1:] {
		pixels := v.Width * v.Height
		if pixels > bestPixels {
			best = v
			bestPixels = pixels
		}
	}
	return best
}

func extensionFromPhotoURL(photoURL string) string {
	lower := strings.ToLower(photoURL)
	switch {
	case strings.Contains(lower, ".png"):
		return "png"
	case strings.Contains(lower, ".webp"):
		return "webp"
	default:
		return "jpg"
	}
}
