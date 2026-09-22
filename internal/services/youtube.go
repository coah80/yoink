package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/coah80/yoink/internal/config"
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

var innertubeHTTP = &http.Client{Timeout: 20 * time.Second}

type YouTubeMeta struct {
	Title     string
	Ext       string
	ID        string
	Uploader  string
	Duration  string
	Thumbnail string
}

type innertubeClient struct {
	Name      string
	Version   string
	Number    string
	UserAgent string
	Extra     map[string]any
}

var innertubeClients = []innertubeClient{
	{
		Name:      "ANDROID",
		Version:   "20.10.38",
		Number:    "3",
		UserAgent: "com.google.android.youtube/20.10.38 (Linux; U; Android 14) gzip",
		Extra:     map[string]any{"androidSdkVersion": 30},
	},
	{
		Name:      "IOS",
		Version:   "20.10.4",
		Number:    "5",
		UserAgent: "com.google.ios.youtube/20.10.4 (iPhone16,2; U; CPU iOS 18_0 like Mac OS X)",
		Extra:     map[string]any{"deviceMake": "Apple", "deviceModel": "iPhone16,2"},
	},
}

type innertubeFormat struct {
	URL      string `json:"url"`
	MimeType string `json:"mimeType"`
	Bitrate  int    `json:"bitrate"`
	Height   int    `json:"height"`
}

type innertubePlayer struct {
	UA     string
	Status string
	Reason string
	Meta   YouTubeMeta
	Muxed  []innertubeFormat
	Adapt  []innertubeFormat
}

type innertubeResponse struct {
	PlayabilityStatus struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"playabilityStatus"`
	VideoDetails struct {
		VideoID       string `json:"videoId"`
		Title         string `json:"title"`
		Author        string `json:"author"`
		LengthSeconds string `json:"lengthSeconds"`
		Thumbnail     struct {
			Thumbnails []struct {
				URL string `json:"url"`
			} `json:"thumbnails"`
		} `json:"thumbnail"`
	} `json:"videoDetails"`
	StreamingData struct {
		Formats         []innertubeFormat `json:"formats"`
		AdaptiveFormats []innertubeFormat `json:"adaptiveFormats"`
	} `json:"streamingData"`
}

func FetchYouTubeMetadata(ctx context.Context, rawURL string) (*YouTubeMeta, error) {
	player, err := fetchInnertubePlayer(ctx, rawURL)
	if err == nil && player.Meta.Title != "" {
		return &player.Meta, nil
	}
	if err != nil {
		log.Printf("[YouTube] innertube metadata failed, trying oembed: %s", err)
	}
	meta, oerr := fetchYouTubeOEmbed(ctx, rawURL)
	if oerr != nil {
		if err != nil {
			return nil, err
		}
		return nil, oerr
	}
	return meta, nil
}

func DownloadYouTubeVideo(ctx context.Context, rawURL, jobID, tempDir, quality string, isAudio bool, progressCb func(float64, int64, int64)) (*DownloadResult, error) {
	player, err := fetchInnertubePlayer(ctx, rawURL)
	if err != nil {
		return nil, err
	}

	maxHeight := config.QualityHeight[quality]
	if maxHeight == 0 {
		maxHeight = 1080
	}

	if isAudio {
		audio := pickYTAudio(player.Adapt)
		if audio.URL == "" {
			muxed := pickYTMuxed(player.Muxed, maxHeight)
			if muxed.URL == "" {
				return nil, fmt.Errorf("no audio formats found")
			}
			out := filepath.Join(tempDir, fmt.Sprintf("%s-yt.m4a", jobID))
			if err := downloadYouTubeFile(ctx, muxed.URL, out, player.UA, progressCb); err != nil {
				return nil, err
			}
			return &DownloadResult{Path: out, Ext: "m4a"}, nil
		}
		ext := ytExtFromMime(audio.MimeType, "m4a")
		out := filepath.Join(tempDir, fmt.Sprintf("%s-yt.%s", jobID, ext))
		if err := downloadYouTubeFile(ctx, audio.URL, out, player.UA, progressCb); err != nil {
			return nil, err
		}
		if err := requireYTFile(out); err != nil {
			return nil, err
		}
		return &DownloadResult{Path: out, Ext: ext}, nil
	}

	video := pickYTVideo(player.Adapt, maxHeight)
	audio := pickYTAudio(player.Adapt)
	if video.URL != "" && audio.URL != "" {
		work := filepath.Join(tempDir, jobID+"-yt-parts")
		if err := os.MkdirAll(work, 0755); err != nil {
			return nil, err
		}
		defer os.RemoveAll(work)

		vPath := filepath.Join(work, "v."+ytExtFromMime(video.MimeType, "mp4"))
		aPath := filepath.Join(work, "a."+ytExtFromMime(audio.MimeType, "m4a"))
		if err := downloadYouTubeFile(ctx, video.URL, vPath, player.UA, func(p float64, d, t int64) {
			if progressCb != nil {
				progressCb(p*0.45, d, t)
			}
		}); err != nil {
			return nil, err
		}
		if err := downloadYouTubeFile(ctx, audio.URL, aPath, player.UA, func(p float64, d, t int64) {
			if progressCb != nil {
				progressCb(45+p*0.45, d, t)
			}
		}); err != nil {
			return nil, err
		}

		out := filepath.Join(tempDir, fmt.Sprintf("%s-yt.mp4", jobID))
		cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", vPath, "-i", aPath, "-c", "copy", "-movflags", "+faststart", out)
		if outb, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[YouTube] [%s] ffmpeg merge failed: %s", jobID, strings.TrimSpace(string(outb)))
			return nil, fmt.Errorf("failed to merge youtube streams")
		}
		if err := requireYTFile(out); err != nil {
			return nil, err
		}
		if info, err := os.Stat(out); err == nil && progressCb != nil {
			progressCb(100, info.Size(), info.Size())
		}
		return &DownloadResult{Path: out, Ext: "mp4"}, nil
	}

	muxed := pickYTMuxed(player.Muxed, maxHeight)
	if muxed.URL == "" {
		return nil, fmt.Errorf("no downloadable formats found")
	}
	ext := ytExtFromMime(muxed.MimeType, "mp4")
	out := filepath.Join(tempDir, fmt.Sprintf("%s-yt.%s", jobID, ext))
	if err := downloadYouTubeFile(ctx, muxed.URL, out, player.UA, progressCb); err != nil {
		return nil, err
	}
	if err := requireYTFile(out); err != nil {
		return nil, err
	}
	return &DownloadResult{Path: out, Ext: ext}, nil
}

func fetchInnertubePlayer(ctx context.Context, rawURL string) (*innertubePlayer, error) {
	videoID := util.ExtractYouTubeVideoID(rawURL)
	if videoID == "" {
		return nil, fmt.Errorf("could not extract YouTube video ID")
	}

	var lastErr error
	for _, client := range innertubeClients {
		player, err := fetchInnertubeClient(ctx, videoID, client)
		if err != nil {
			lastErr = err
			continue
		}
		if player.Status != "" && player.Status != "OK" {
			reason := player.Reason
			if reason == "" {
				reason = player.Status
			}
			lastErr = fmt.Errorf("%s", reason)
			continue
		}
		if player.Meta.Title == "" && len(player.Muxed)+len(player.Adapt) == 0 {
			lastErr = fmt.Errorf("empty innertube response")
			continue
		}
		log.Printf("[YouTube] innertube %s ok for %s", client.Name, videoID)
		return player, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("all innertube clients failed")
	}
	return nil, lastErr
}

func fetchInnertubeClient(ctx context.Context, videoID string, client innertubeClient) (*innertubePlayer, error) {
	cli := map[string]any{
		"clientName":    client.Name,
		"clientVersion": client.Version,
		"hl":            "en",
		"gl":            "US",
	}
	for k, v := range client.Extra {
		cli[k] = v
	}
	body, _ := json.Marshal(map[string]any{
		"context":        map[string]any{"client": cli},
		"videoId":        videoID,
		"contentCheckOk": true,
		"racyCheckOk":    true,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "https://www.youtube.com/youtubei/v1/player?prettyPrint=false", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", client.UserAgent)
	req.Header.Set("X-YouTube-Client-Name", client.Number)
	req.Header.Set("X-YouTube-Client-Version", client.Version)

	resp, err := innertubeHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("innertube HTTP %d", resp.StatusCode)
	}

	var parsed innertubeResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("invalid innertube JSON")
	}

	thumb := ""
	if thumbs := parsed.VideoDetails.Thumbnail.Thumbnails; len(thumbs) > 0 {
		thumb = thumbs[len(thumbs)-1].URL
	}

	title := parsed.VideoDetails.Title
	if title == "" {
		title = "download"
	}
	id := parsed.VideoDetails.VideoID
	if id == "" {
		id = videoID
	}

	return &innertubePlayer{
		UA:     client.UserAgent,
		Status: parsed.PlayabilityStatus.Status,
		Reason: parsed.PlayabilityStatus.Reason,
		Meta: YouTubeMeta{
			Title:     title,
			Ext:       "mp4",
			ID:        id,
			Uploader:  parsed.VideoDetails.Author,
			Duration:  parsed.VideoDetails.LengthSeconds,
			Thumbnail: thumb,
		},
		Muxed: withYTURL(parsed.StreamingData.Formats),
		Adapt: withYTURL(parsed.StreamingData.AdaptiveFormats),
	}, nil
}

func fetchYouTubeOEmbed(ctx context.Context, rawURL string) (*YouTubeMeta, error) {
	watchURL := util.NormalizeYouTubeURL(rawURL)
	api := "https://www.youtube.com/oembed?format=json&url=" + url.QueryEscape(watchURL)

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", api, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Yoink/1.0)")

	resp, err := innertubeHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("oembed HTTP %d", resp.StatusCode)
	}

	var oembed struct {
		Title     string `json:"title"`
		Author    string `json:"author_name"`
		Thumbnail string `json:"thumbnail_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&oembed); err != nil {
		return nil, err
	}
	if oembed.Title == "" {
		return nil, fmt.Errorf("empty oembed title")
	}

	return &YouTubeMeta{
		Title:     oembed.Title,
		Ext:       "mp4",
		ID:        util.ExtractYouTubeVideoID(watchURL),
		Uploader:  oembed.Author,
		Thumbnail: oembed.Thumbnail,
	}, nil
}

func downloadYouTubeFile(ctx context.Context, fileURL, outputPath, ua string, progressCb func(float64, int64, int64)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		return err
	}
	if ua == "" {
		ua = innertubeClients[0].UserAgent
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Referer", "https://www.youtube.com/")

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
		return err
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
				return writeErr
			}
			downloaded += int64(n)
			if progressCb != nil && totalSize > 0 {
				progressCb(float64(downloaded)/float64(totalSize)*100, downloaded, totalSize)
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

func pickYTVideo(formats []innertubeFormat, maxHeight int) innertubeFormat {
	var best innertubeFormat
	var bestScore int
	for _, f := range formats {
		if f.URL == "" || !strings.HasPrefix(f.MimeType, "video/") || f.Height <= 0 {
			continue
		}
		if maxHeight > 0 && f.Height > maxHeight {
			continue
		}
		score := f.Bitrate
		if strings.Contains(f.MimeType, "avc1") {
			score += 1_000_000_000
		}
		if best.URL == "" || score > bestScore {
			best = f
			bestScore = score
		}
	}
	return best
}

func pickYTAudio(formats []innertubeFormat) innertubeFormat {
	var best innertubeFormat
	var bestScore int
	for _, f := range formats {
		if f.URL == "" || !strings.HasPrefix(f.MimeType, "audio/") {
			continue
		}
		score := f.Bitrate
		if strings.Contains(f.MimeType, "mp4a") || strings.Contains(f.MimeType, "audio/mp4") {
			score += 1_000_000_000
		}
		if best.URL == "" || score > bestScore {
			best = f
			bestScore = score
		}
	}
	return best
}

func pickYTMuxed(formats []innertubeFormat, maxHeight int) innertubeFormat {
	var best innertubeFormat
	for _, f := range formats {
		if f.URL == "" {
			continue
		}
		if maxHeight > 0 && f.Height > maxHeight && f.Height > 0 {
			continue
		}
		if best.URL == "" || f.Bitrate > best.Bitrate {
			best = f
		}
	}
	return best
}

func withYTURL(in []innertubeFormat) []innertubeFormat {
	out := make([]innertubeFormat, 0, len(in))
	for _, f := range in {
		if f.URL != "" {
			out = append(out, f)
		}
	}
	return out
}

func ytExtFromMime(mime, fallback string) string {
	mime = strings.ToLower(mime)
	switch {
	case strings.Contains(mime, "audio/mp4"), strings.Contains(mime, "mp4a"):
		return "m4a"
	case strings.Contains(mime, "audio/webm"):
		return "webm"
	case strings.Contains(mime, "video/mp4"):
		return "mp4"
	case strings.Contains(mime, "video/webm"):
		return "webm"
	default:
		return fallback
	}
}

func requireYTFile(path string) error {
	info, err := os.Stat(path)
	if err != nil || info.Size() < 1000 {
		os.Remove(path)
		return fmt.Errorf("downloaded file too small or missing")
	}
	return nil
}
