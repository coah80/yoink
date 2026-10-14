package services

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/util"
)

var percentRe = regexp.MustCompile(`([\d.]+)%`)
var speedRe = regexp.MustCompile(`at\s+([\d.]+\s*\w+/s)`)
var etaRe = regexp.MustCompile(`ETA\s+(\S+)`)
var ytdlpErrorRe = regexp.MustCompile(`(?i)ERROR[:\s]+(.+?)(?:\n|$)`)

func isTwitchVODURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "twitch.tv" && !strings.HasSuffix(host, ".twitch.tv") {
		return false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	return len(parts) == 2 && parts[0] == "videos" && parts[1] != ""
}

type YtdlpProgress struct {
	Percent float64
	Speed   string
	ETA     string
}

func ParseYtdlpProgress(text string) YtdlpProgress {
	var p YtdlpProgress
	if m := percentRe.FindStringSubmatch(text); len(m) > 1 {
		p.Percent, _ = strconv.ParseFloat(m[1], 64)
		if p.Percent > 100 {
			p.Percent = 0
		}
	}
	if m := speedRe.FindStringSubmatch(text); len(m) > 1 {
		p.Speed = m[1]
	}
	if m := etaRe.FindStringSubmatch(text); len(m) > 1 {
		p.ETA = m[1]
	}
	return p
}

type DownloadOpts struct {
	DownloadPreferences
	IsAudio     bool
	AudioFormat string
	Quality     string
	Container   string
	TempDir     string
	FilePrefix  string
	ProcessInfo *ProcessInfo
	Playlist    bool
	UseProxy    bool
	OnProgress  func(percent float64, speed, eta string)
	OnCancel    func(killFn func())
}

type DownloadResult struct {
	Path string
	Ext  string
}

func isYouTubeURL(rawURL string) bool {
	return strings.Contains(rawURL, "youtube.com") || strings.Contains(rawURL, "youtu.be")
}

func DownloadViaYtdlp(ctx context.Context, url, jobID string, opts DownloadOpts) (*DownloadResult, error) {
	if opts.AudioFormat == "" {
		opts.AudioFormat = "mp3"
	}
	if opts.Quality == "" {
		opts.Quality = "1080p"
	}
	if opts.Container == "" {
		opts.Container = "mp4"
	}

	if isYouTubeURL(url) && util.HasProxy() {
		opts.UseProxy = true
	}

	filePrefix := fmt.Sprintf("%s%s", opts.FilePrefix, jobID)
	tries := 1
	if opts.UseProxy && util.HasProxy() {
		tries = 3
	}

	var lastErr error
	for attempt := 1; attempt <= tries; attempt++ {
		if attempt > 1 {
			cleanupYtdlpOutputs(opts.TempDir, filePrefix)
			log.Printf("[%s] yt-dlp retry %d/%d with a new proxy", jobID, attempt, tries)
		}
		result, err := downloadViaYtdlpOnce(ctx, url, jobID, opts)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if ctx.Err() != nil || (opts.ProcessInfo != nil && opts.ProcessInfo.IsCancelled()) {
			return nil, err
		}
		if !util.ShouldRetryWithNewProxy(err.Error()) {
			return nil, err
		}
	}
	return nil, lastErr
}

func downloadViaYtdlpOnce(ctx context.Context, url, jobID string, opts DownloadOpts) (*DownloadResult, error) {
	filePrefix := fmt.Sprintf("%s%s", opts.FilePrefix, jobID)
	maxHeight := config.QualityHeight[opts.Quality]
	resolved, handled, err := ResolveSocialMedia(ctx, url, maxHeight)
	if err != nil {
		return nil, err
	}
	if handled {
		url = resolved.DirectURL
	}
	url = util.NormalizeYtdlpURL(url)
	directGIF := strings.HasSuffix(strings.ToLower(strings.SplitN(url, "?", 2)[0]), ".gif")

	tempFile := filepath.Join(opts.TempDir, fmt.Sprintf("%s.%%(ext)s", filePrefix))

	args := append([]string{}, util.YtdlpArgsForURL(url)...)
	if isTwitchVODURL(url) {
		args = append(args, "--downloader", "m3u8:ffmpeg")
	}
	if resolved != nil {
		if resolved.UserAgent != "" {
			args = append(args, "--user-agent", resolved.UserAgent)
		}
		if resolved.Referer != "" {
			args = append(args, "--referer", resolved.Referer)
		}
	}
	if opts.UseProxy {
		args = append(args, util.GetProxyArgs()...)
	}
	args = append(args, "--continue", "--remote-components", "ejs:github")
	if !util.IsBilibiliURL(url) && (resolved == nil || !resolved.SkipSleep) {
		args = append(args, "-t", "sleep")
	}
	if opts.Playlist {
		args = append(args, "--yes-playlist")
	} else {
		args = append(args, "--no-playlist")
	}
	args = append(args,
		"--newline",
		"--progress-template", "%(progress._percent_str)s",
		"-o", tempFile,
	)

	if opts.IsAudio {
		audioSelector := "bestaudio/best"
		if strings.Contains(strings.ToLower(url), "tiktok.com/") {
			audioSelector = "download/b[acodec!=none][vcodec^=h264]/bestaudio/best"
		}
		args = append(args, "-f", audioSelector)
	} else {
		if directGIF {
			args = append(args, "-f", "best")
		} else {
			codec := opts.EffectiveCodec(opts.Container)
			if opts.H265 && (codec == "" || codec == "h264") && !strings.EqualFold(opts.Container, "webm") {
				codec = "h265"
			}
			args = append(args, VideoFormatArgs(opts.Quality, codec, opts.Container)...)
			args = append(args, "--merge-output-format", opts.Container)
		}
	}

	if opts.Metadata != nil {
		if *opts.Metadata {
			args = append(args, "--embed-metadata")
		} else {
			args = append(args, "--no-embed-metadata")
		}
	}
	args = append(args, url)

	cmd := exec.CommandContext(ctx, util.YtdlpBinary(), args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	stopCancellation := ConfigureYtdlpCancellation(cmd, 5*time.Second, stdout, stderr)
	defer stopCancellation()

	if err := opts.ProcessInfo.StartCmd(cmd); err != nil {
		return nil, fmt.Errorf("failed to start yt-dlp: %w", err)
	}

	if opts.OnCancel != nil {
		opts.OnCancel(func() {
			if cmd.Process != nil {
				_ = cmd.Cancel()
			}
		})
	}

	var stderrOutput strings.Builder
	var lastProgress float64
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			p := ParseYtdlpProgress(line)
			mu.Lock()
			shouldReport := p.Percent > 0 && (p.Percent > lastProgress+2 || p.Percent >= 100)
			if shouldReport {
				lastProgress = p.Percent
			}
			mu.Unlock()
			if shouldReport && opts.OnProgress != nil {
				opts.OnProgress(p.Percent, p.Speed, p.ETA)
			}
		}
	}()

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			stderrOutput.WriteString(line + "\n")
			if strings.Contains(line, "[download]") && strings.Contains(line, "%") {
				p := ParseYtdlpProgress(line)
				mu.Lock()
				shouldReport := p.Percent > 0 && (p.Percent > lastProgress+2 || p.Percent >= 100)
				if shouldReport {
					lastProgress = p.Percent
				}
				mu.Unlock()
				if shouldReport && opts.OnProgress != nil {
					opts.OnProgress(p.Percent, p.Speed, p.ETA)
				}
			}
		}
	}()

	wg.Wait()
	err = cmd.Wait()

	if opts.ProcessInfo != nil && opts.ProcessInfo.IsCancelled() {
		return nil, fmt.Errorf("Download cancelled")
	}

	if err != nil {
		if isVimeoPlayerURL(url) {
			if result, fallbackErr := downloadVimeoFallback(ctx, url, jobID, maxHeight, opts); fallbackErr == nil {
				return result, nil
			}
		}
		errMsg := "Download failed"
		if m := ytdlpErrorRe.FindStringSubmatch(stderrOutput.String()); len(m) > 1 {
			errMsg = strings.TrimSpace(m[1])
		}
		if util.NeedsCookiesRetry(errMsg) && util.RefreshCookies("YouTube auth failure during download") {
			cleanupYtdlpOutputs(opts.TempDir, filePrefix)
			return downloadViaYtdlpOnce(ctx, url, jobID, opts)
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	entries, err := os.ReadDir(opts.TempDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read temp dir: %w", err)
	}

	prefix := filePrefix
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if strings.Contains(name, "-final") || strings.Contains(name, "-cobalt") ||
			strings.Contains(name, "-clip") || strings.Contains(name, "-trimmed") ||
			strings.HasSuffix(name, ".part") || strings.Contains(name, ".part-Frag") {
			continue
		}
		fullPath := filepath.Join(opts.TempDir, name)
		ext := strings.TrimPrefix(filepath.Ext(name), ".")
		return &DownloadResult{Path: fullPath, Ext: ext}, nil
	}

	return nil, fmt.Errorf("Downloaded file not found")
}

// VideoFormatArgs prioritizes resolution before codec preference. A codec-only
// fallback chain can select 480p H.264 even when 1080p VP9/AV1 is available.
func VideoFormatArgs(quality, codec, container string) []string {
	if quality == "" {
		quality = "1080p"
	}
	if quality == "4k" { // Accept settings saved by older frontends.
		quality = "2160p"
	}
	filter, portrait := "", ""
	if height := config.QualityHeight[quality]; height > 0 {
		filter = fmt.Sprintf("[height<=%d]", height)
		portrait = fmt.Sprintf("[width<=%d]", height)
	}
	video, audio := "bv*", "ba"
	combined := "b"
	if container == "webm" {
		video += "[ext=webm]"
		audio += "[ext=webm]"
		combined += "[ext=webm]"
	}
	sort := "res,fps"
	switch codec {
	case "av1":
		sort += ",vcodec:av01,acodec:opus"
	case "vp9":
		sort += ",vcodec:vp9,acodec:opus"
	case "h265":
		sort += ",vcodec:hevc,+acodec:m4a"
	default:
		sort += ",+vcodec:avc,+acodec:m4a"
	}
	selector := video + filter + "+" + audio
	if portrait != "" {
		selector += "/" + video + portrait + "+" + audio
	}
	selector += "/" + combined + filter
	if portrait != "" {
		selector += "/" + combined + portrait
	}
	return []string{"-f", selector, "-S", sort}
}

func cleanupYtdlpOutputs(tempDir, prefix string) {
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			_ = os.Remove(filepath.Join(tempDir, entry.Name()))
		}
	}
}

type PlaylistInfo struct {
	Title   string
	Entries []PlaylistEntry
	Count   int
}

type PlaylistEntry struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	ID    string `json:"id"`
}

func GetPlaylistInfo(ctx context.Context, url string, useProxy bool) (*PlaylistInfo, error) {
	url = util.NormalizeYtdlpURL(url)
	args := append([]string{}, util.YtdlpArgsForURL(url)...)
	if useProxy {
		args = append(args, util.GetProxyArgs()...)
	}
	if !util.IsBilibiliURL(url) {
		args = append(args, "-t", "sleep")
	}
	args = append(args, "--yes-playlist", "--flat-playlist", "-J", url)

	cmd := exec.CommandContext(ctx, util.YtdlpBinary(), args...)
	stopCancellation := ConfigureYtdlpCancellation(cmd, 5*time.Second)
	defer stopCancellation()
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			errMsg := "Failed to get playlist info"
			if m := ytdlpErrorRe.FindStringSubmatch(string(exitErr.Stderr)); len(m) > 1 {
				errMsg = strings.TrimSpace(m[1])
			}
			return nil, fmt.Errorf("%s", errMsg)
		}
		return nil, err
	}

	type rawPlaylist struct {
		Title         string          `json:"title"`
		Entries       []PlaylistEntry `json:"entries"`
		PlaylistCount int             `json:"playlist_count"`
	}

	var raw rawPlaylist
	if err := jsonUnmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("Failed to parse playlist info")
	}

	count := raw.PlaylistCount
	if count == 0 {
		count = len(raw.Entries)
	}

	return &PlaylistInfo{
		Title:   OrDefault(raw.Title, "Playlist"),
		Entries: raw.Entries,
		Count:   count,
	}, nil
}

func DownloadClipViaYtdlp(ctx context.Context, clipData *ClipData, jobID string, tempDir, quality, codec, container string, onProgress func(float64, string, string)) (*DownloadResult, error) {
	startTime := float64(clipData.StartTimeMs) / 1000
	endTime := float64(clipData.EndTimeMs) / 1000
	clipFile := filepath.Join(tempDir, fmt.Sprintf("%s-ytclip.%%(ext)s", jobID))

	args := append([]string{}, util.YtdlpArgsForURL(clipData.FullVideoURL)...)
	args = append(args, util.GetProxyArgs()...)
	args = append(args, VideoFormatArgs(quality, codec, container)...)
	args = append(args,
		"--remote-components", "ejs:github",
		"--no-playlist",
		"--download-sections", fmt.Sprintf("*%g-%g", startTime, endTime),
		"--force-keyframes-at-cuts",
		"--merge-output-format", container,
		"--newline",
		"--progress-template", "%(progress._percent_str)s",
		"-o", clipFile,
		clipData.FullVideoURL,
	)

	cmd := exec.CommandContext(ctx, util.YtdlpBinary(), args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	stopCancellation := ConfigureYtdlpCancellation(cmd, 5*time.Second, stdout, stderr)
	defer stopCancellation()

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var stderrOutput strings.Builder
	var lastProgress float64
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			p := ParseYtdlpProgress(scanner.Text())
			mu.Lock()
			shouldReport := p.Percent > 0 && (p.Percent > lastProgress+2 || p.Percent >= 100)
			if shouldReport {
				lastProgress = p.Percent
			}
			mu.Unlock()
			if shouldReport && onProgress != nil {
				onProgress(p.Percent, p.Speed, p.ETA)
			}
		}
	}()

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			stderrOutput.WriteString(line + "\n")
			if strings.Contains(line, "[download]") && strings.Contains(line, "%") {
				p := ParseYtdlpProgress(line)
				mu.Lock()
				shouldReport := p.Percent > 0 && (p.Percent > lastProgress+2 || p.Percent >= 100)
				if shouldReport {
					lastProgress = p.Percent
				}
				mu.Unlock()
				if shouldReport && onProgress != nil {
					onProgress(p.Percent, p.Speed, p.ETA)
				}
			}
		}
	}()

	wg.Wait()
	if err := cmd.Wait(); err != nil {
		errMsg := "yt-dlp clip download failed"
		if m := ytdlpErrorRe.FindStringSubmatch(stderrOutput.String()); len(m) > 1 {
			errMsg = strings.TrimSpace(m[1])
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	entries, _ := os.ReadDir(tempDir)
	prefix := fmt.Sprintf("%s-ytclip", jobID)
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, prefix) && !strings.HasSuffix(name, ".part") && !strings.Contains(name, ".part-Frag") {
			fullPath := filepath.Join(tempDir, name)
			info, err := os.Stat(fullPath)
			if err != nil || info.Size() < 10000 {
				os.Remove(fullPath)
				return nil, fmt.Errorf("yt-dlp clip output too small")
			}
			log.Printf("[%s] yt-dlp clip download complete: %.2fMB", jobID, float64(info.Size())/1024/1024)
			ext := strings.TrimPrefix(filepath.Ext(name), ".")
			if ext == "" {
				ext = "mp4"
			}
			return &DownloadResult{Path: fullPath, Ext: ext}, nil
		}
	}

	return nil, fmt.Errorf("yt-dlp clip file not found")
}

func HandleClipDownload(ctx context.Context, clipData *ClipData, jobID string, tempDir, quality, codec, container string, onProgress func(float64, string, string)) (*DownloadResult, error) {
	log.Printf("[%s] Trying yt-dlp --download-sections...", jobID)
	result, err := DownloadClipViaYtdlp(ctx, clipData, jobID, tempDir, quality, codec, container, onProgress)
	if err == nil {
		return result, nil
	}
	if ctx.Err() != nil || !util.HasProxy() || !util.ShouldRetryWithNewProxy(err.Error()) {
		return nil, err
	}
	log.Printf("[%s] yt-dlp clip failed, retrying with a new proxy: %s", jobID, err)
	cleanupYtdlpOutputs(tempDir, jobID+"-ytclip")
	if result, err = DownloadClipViaYtdlp(ctx, clipData, jobID, tempDir, quality, codec, container, onProgress); err == nil {
		return result, nil
	}
	log.Printf("[%s] yt-dlp clip retry failed: %s", jobID, err)
	return nil, err
}

func OrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
