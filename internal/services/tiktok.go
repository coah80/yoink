package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var tiktokMusicIDRe = regexp.MustCompile(`/music/[^/]*?-(\d+)`)

var tiktokURLRe = regexp.MustCompile(`(?i)(?:^|://)(?:www\.|vm\.|vt\.)?tiktok\.com/`)

type TikTokMeta struct {
	Title       string
	Duration    int
	Author      string
	Thumbnail   string
	IsSlideshow bool
	ImageCount  int
}

type tikwmVideoResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Duration int    `json:"duration"`
		Play     string `json:"play"`
		WMPlay   string `json:"wmplay"`
		Music    string `json:"music"`
		Images   []struct {
			URL string `json:"url"`
		} `json:"images"`
		Author struct {
			UniqueID string `json:"unique_id"`
			Nickname string `json:"nickname"`
			Avatar   string `json:"avatar"`
		} `json:"author"`
		Cover string `json:"cover"`
	} `json:"data"`
}

type tikwmMusicResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Videos []struct {
			Title string `json:"title"`
			Music string `json:"music"`
		} `json:"videos"`
	} `json:"data"`
}

func IsTikTokURL(rawURL string) bool {
	return tiktokURLRe.MatchString(rawURL)
}

func IsTikTokMusicURL(rawURL string) bool {
	return (strings.Contains(rawURL, "tiktok.com/music/") || strings.Contains(rawURL, "tiktok.com/music?")) &&
		tiktokMusicIDRe.MatchString(rawURL)
}

func ExtractTikTokMusicID(rawURL string) string {
	m := tiktokMusicIDRe.FindStringSubmatch(rawURL)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

func fetchTikWMVideo(ctx context.Context, rawURL string) (*tikwmVideoResponse, error) {
	apiURL := "https://www.tikwm.com/api/?url=" + url.QueryEscape(rawURL)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create tikwm request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tikwm request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read tikwm response: %w", err)
	}

	var result tikwmVideoResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tikwm response: %w", err)
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("tikwm error: %s", result.Msg)
	}

	return &result, nil
}

func FetchTikTokMetadata(ctx context.Context, rawURL string) (*TikTokMeta, error) {
	if ExtractorAvailable() {
		extData, err := ExtractTikTok(ctx, rawURL)
		if err == nil {
			meta := &TikTokMeta{
				Title:       extData.Title,
				Duration:    extData.Duration,
				Author:      extData.Author,
				Thumbnail:   extData.Thumbnail,
				IsSlideshow: extData.Type == "slideshow",
				ImageCount:  len(extData.Images),
			}
			return meta, nil
		}
		log.Printf("[TikTok] Extractor failed, falling back to tikwm: %s", err)
	}

	result, err := fetchTikWMVideo(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	meta := &TikTokMeta{
		Title:     result.Data.Title,
		Duration:  result.Data.Duration,
		Thumbnail: result.Data.Cover,
	}

	if result.Data.Author.Nickname != "" {
		meta.Author = result.Data.Author.Nickname
	} else if result.Data.Author.UniqueID != "" {
		meta.Author = result.Data.Author.UniqueID
	}

	if len(result.Data.Images) > 0 {
		meta.IsSlideshow = true
		meta.ImageCount = len(result.Data.Images)
	}

	return meta, nil
}

func DownloadTikTokVideo(ctx context.Context, rawURL, jobID, tempDir string, isAudio bool, progressCb func(float64, int64, int64)) (*DownloadResult, error) {
	if ExtractorAvailable() {
		if isAudio {
			extData, err := ExtractTikTok(ctx, rawURL)
			if err == nil && extData.AudioURL != "" {
				log.Printf("[TikTok] [%s] Extractor got audio URL, downloading", jobID)
				outputPath := filepath.Join(tempDir, fmt.Sprintf("%s-tiktok.mp3", jobID))
				if dlErr := downloadFileWithProgress(ctx, extData.AudioURL, outputPath, progressCb); dlErr == nil {
					return &DownloadResult{Path: outputPath, Ext: "mp3"}, nil
				}
			}
		} else {
			extData, err := ExtractTikTok(ctx, rawURL)
			if err == nil && extData.FilePath != "" {
				log.Printf("[TikTok] [%s] Extractor captured video: %d bytes", jobID, extData.FileSize)
				destPath := filepath.Join(tempDir, fmt.Sprintf("%s-tiktok.mp4", jobID))
				if cpErr := copyFile(extData.FilePath, destPath); cpErr == nil {
					os.Remove(extData.FilePath)
					return &DownloadResult{Path: destPath, Ext: "mp4"}, nil
				}
				log.Printf("[TikTok] [%s] Failed to copy extractor file: %s", jobID, err)
			}
			if err != nil {
				log.Printf("[TikTok] [%s] Extractor failed, falling back to tikwm: %s", jobID, err)
			}
		}
	}

	log.Printf("[TikTok] [%s] Fetching video info from tikwm", jobID)

	result, err := fetchTikWMVideo(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get TikTok video info: %w", err)
	}

	var downloadURL, ext string

	if isAudio {
		downloadURL = result.Data.Music
		ext = "mp3"
		if downloadURL == "" {
			return nil, fmt.Errorf("no audio available for this TikTok")
		}
		log.Printf("[TikTok] [%s] Downloading audio: %s", jobID, result.Data.Title)
	} else {
		downloadURL = result.Data.Play
		ext = "mp4"
		if downloadURL == "" {
			downloadURL = result.Data.WMPlay
		}
		if downloadURL == "" {
			return nil, fmt.Errorf("no video URL available for this TikTok")
		}
		log.Printf("[TikTok] [%s] Downloading video (no watermark): %s", jobID, result.Data.Title)
	}

	outputPath := filepath.Join(tempDir, fmt.Sprintf("%s-tiktok.%s", jobID, ext))

	if err := downloadFileWithProgress(ctx, downloadURL, outputPath, progressCb); err != nil {
		return nil, err
	}

	info, statErr := os.Stat(outputPath)
	if statErr != nil || info.Size() < 1000 {
		os.Remove(outputPath)
		return nil, fmt.Errorf("downloaded file too small or missing")
	}

	log.Printf("[TikTok] [%s] Downloaded: %.2fMB", jobID, float64(info.Size())/1024/1024)
	return &DownloadResult{Path: outputPath, Ext: ext}, nil
}

func fetchTikTokMusicURL(ctx context.Context, musicID string) (string, string, error) {
	apiURL := fmt.Sprintf("https://www.tikwm.com/api/music/posts/?music_id=%s&count=1", musicID)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("tikwm request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read tikwm response: %w", err)
	}

	var result tikwmMusicResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("failed to parse tikwm response: %w", err)
	}

	if result.Code != 0 {
		return "", "", fmt.Errorf("tikwm error: %s", result.Msg)
	}

	if len(result.Data.Videos) == 0 {
		return "", "", fmt.Errorf("no videos found for this sound")
	}

	musicURL := result.Data.Videos[0].Music
	title := result.Data.Videos[0].Title
	if musicURL == "" {
		return "", "", fmt.Errorf("no music URL in response")
	}

	return musicURL, title, nil
}

func DownloadTikTokMusic(ctx context.Context, rawURL, jobID, tempDir string, progressCb func(float64, int64, int64)) (*DownloadResult, error) {
	musicID := ExtractTikTokMusicID(rawURL)
	if musicID == "" {
		return nil, fmt.Errorf("could not extract TikTok music ID from URL")
	}

	log.Printf("[TikTok] [%s] Fetching music URL for ID: %s", jobID, musicID)

	musicURL, title, err := fetchTikTokMusicURL(ctx, musicID)
	if err != nil {
		return nil, fmt.Errorf("failed to get TikTok audio: %w", err)
	}

	log.Printf("[TikTok] [%s] Got music URL, title: %s", jobID, title)

	outputPath := filepath.Join(tempDir, fmt.Sprintf("%s-tiktok.mp3", jobID))

	if err := downloadFileWithProgress(ctx, musicURL, outputPath, progressCb); err != nil {
		return nil, err
	}

	info, statErr := os.Stat(outputPath)
	if statErr != nil || info.Size() < 1000 {
		os.Remove(outputPath)
		return nil, fmt.Errorf("downloaded audio file too small or missing")
	}

	log.Printf("[TikTok] [%s] Audio downloaded: %.2fMB", jobID, float64(info.Size())/1024/1024)
	return &DownloadResult{Path: outputPath, Ext: "mp3"}, nil
}

func downloadFileWithProgress(ctx context.Context, fileURL, outputPath string, progressCb func(float64, int64, int64)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	totalSize := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)

	for {
		if ctx.Err() != nil {
			os.Remove(outputPath)
			return fmt.Errorf("cancelled")
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				os.Remove(outputPath)
				return fmt.Errorf("failed to write file: %w", writeErr)
			}
			downloaded += int64(n)

			if progressCb != nil && totalSize > 0 {
				progress := float64(downloaded) / float64(totalSize) * 100
				progressCb(progress, downloaded, totalSize)
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				os.Remove(outputPath)
				return fmt.Errorf("download interrupted: %w", readErr)
			}
			break
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
