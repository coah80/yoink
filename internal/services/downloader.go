package services

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/util"
)

var percentRe = regexp.MustCompile(`([\d.]+)%`)
var speedRe = regexp.MustCompile(`at\s+([\d.]+\s*\w+/s)`)
var etaRe = regexp.MustCompile(`ETA\s+(\S+)`)
var ytdlpErrorRe = regexp.MustCompile(`(?i)ERROR[:\s]+(.+?)(?:\n|$)`)

type YtdlpProgress struct {
	Percent float64
	Speed   string
	ETA     string
}

func ParseYtdlpProgress(text string) YtdlpProgress {
	var p YtdlpProgress
	if m := percentRe.FindStringSubmatch(text); len(m) > 1 {
		p.Percent, _ = strconv.ParseFloat(m[1], 64)
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
	IsAudio      bool
	AudioFormat  string
	Quality      string
	Container    string
	TempDir      string
	FilePrefix   string
	ProcessInfo  *ProcessInfo
	Playlist     bool
	UseProxy     bool
	OnProgress   func(percent float64, speed, eta string)
	OnCancel     func(killFn func())
}

type DownloadResult struct {
	Path string
	Ext  string
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

	tempFile := filepath.Join(opts.TempDir, fmt.Sprintf("%s%s.%%(ext)s", opts.FilePrefix, jobID))

	args := append([]string{}, util.GetYouTubeAuthArgs()...)
	if opts.UseProxy {
		args = append(args, util.GetProxyArgs()...)
	}
	args = append(args,
		"--continue",
		"-t", "sleep",
	)
	if opts.Playlist {
		args = append(args, "--yes-playlist")
	} else {
		args = append(args, "--no-playlist")
	}
	args = append(args,
		"--newline",
		"--progress-template", "%(progress._percent_str)s",
		"-o", tempFile,
		"--ffmpeg-location", "/usr/bin/ffmpeg",
	)

	if opts.IsAudio {
		args = append(args, "-f", "bestaudio/best")
	} else {
		maxHeight := config.QualityHeight[opts.Quality]
		if maxHeight > 0 {
			args = append(args, "-f",
				fmt.Sprintf("bv[vcodec^=avc][height<=%d]+ba[acodec^=mp4a]/bv[height<=%d]+ba/b", maxHeight, maxHeight))
		} else {
			args = append(args, "-f", "bv[vcodec^=avc]+ba[acodec^=mp4a]/bv+ba/b")
		}
		args = append(args, "--merge-output-format", opts.Container)
	}

	args = append(args, url)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	if opts.ProcessInfo != nil {
		opts.ProcessInfo.SetCmd(cmd)
	}

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start yt-dlp: %w", err)
	}

	if opts.OnCancel != nil {
		opts.OnCancel(func() {
			if cmd.Process != nil {
				cmd.Process.Kill()
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
	err := cmd.Wait()

	if opts.ProcessInfo != nil && opts.ProcessInfo.IsCancelled() {
		return nil, fmt.Errorf("Download cancelled")
	}

	if err != nil {
		errMsg := "Download failed"
		if m := ytdlpErrorRe.FindStringSubmatch(stderrOutput.String()); len(m) > 1 {
			errMsg = strings.TrimSpace(m[1])
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	entries, err := os.ReadDir(opts.TempDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read temp dir: %w", err)
	}

	prefix := fmt.Sprintf("%s%s", opts.FilePrefix, jobID)
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
	args := append([]string{}, util.GetYouTubeAuthArgs()...)
	if useProxy {
		args = append(args, util.GetProxyArgs()...)
	}
	args = append(args, "-t", "sleep", "--yes-playlist", "--flat-playlist", "-J", url)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
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

func DownloadClipViaYtdlp(ctx context.Context, clipData *ClipData, jobID string, tempDir string, onProgress func(float64, string, string)) (*DownloadResult, error) {
	startTime := float64(clipData.StartTimeMs) / 1000
	endTime := float64(clipData.EndTimeMs) / 1000
	clipFile := filepath.Join(tempDir, fmt.Sprintf("%s-ytclip.%%(ext)s", jobID))

	args := append([]string{}, util.GetYouTubeAuthArgs()...)
	args = append(args, util.GetProxyArgs()...)
	args = append(args,
		"--no-playlist",
		"--download-sections", fmt.Sprintf("*%g-%g", startTime, endTime),
		"--force-keyframes-at-cuts",
		"-f", "bv[vcodec^=avc][height<=1080]+ba[acodec^=mp4a]/bv[height<=1080]+ba/b",
		"--merge-output-format", "mp4",
		"--newline",
		"--progress-template", "%(progress._percent_str)s",
		"-o", clipFile,
		"--ffmpeg-location", "/usr/bin/ffmpeg",
		clipData.FullVideoURL,
	)

	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

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

func HandleClipDownload(ctx context.Context, clipData *ClipData, jobID string, tempDir string, onProgress func(float64, string, string)) (*DownloadResult, error) {
	startTime := float64(clipData.StartTimeMs) / 1000
	endTime := float64(clipData.EndTimeMs) / 1000
	duration := float64(clipData.EndTimeMs-clipData.StartTimeMs) / 1000
	clipFile := filepath.Join(tempDir, fmt.Sprintf("%s-clip.mp4", jobID))

	log.Printf("[%s] Trying yt-dlp --download-sections...", jobID)
	result, err := DownloadClipViaYtdlp(ctx, clipData, jobID, tempDir, onProgress)
	if err == nil {
		return result, nil
	}
	log.Printf("[%s] yt-dlp clip failed: %s", jobID, err)

	result2, err := StreamClipFromCobalt(ctx, clipData.FullVideoURL, jobID, startTime, endTime, clipFile, func(progress float64) {
		if onProgress != nil {
			onProgress(progress, "", "")
		}
	})
	if err == nil {
		return &DownloadResult{Path: result2.FilePath, Ext: result2.Ext}, nil
	}
	log.Printf("[%s] Stream trim failed: %s", jobID, err)

	log.Printf("[%s] Falling back to full cobalt download + trim...", jobID)
	cobaltResult, err := DownloadViaCobalt(ctx, clipData.FullVideoURL, jobID, false, func(progress float64, downloaded, total int64) {
		if onProgress != nil {
			onProgress(progress*0.8, "", "")
		}
	}, CobaltDownloadOpts{OutputDir: tempDir})
	if err != nil {
		return nil, err
	}

	trimmedFile := filepath.Join(tempDir, fmt.Sprintf("%s-trimmed.%s", jobID, cobaltResult.Ext))

	trimCmd := exec.CommandContext(ctx, "ffmpeg",
		"-ss", fmt.Sprintf("%g", startTime),
		"-i", cobaltResult.FilePath,
		"-t", fmt.Sprintf("%g", duration),
		"-c", "copy",
		"-avoid_negative_ts", "make_zero",
		"-y",
		trimmedFile,
	)
	if err := trimCmd.Run(); err != nil {
		return nil, fmt.Errorf("Trim failed")
	}

	os.Remove(cobaltResult.FilePath)

	info, err := os.Stat(trimmedFile)
	if err != nil || info.Size() < 10000 {
		os.Remove(trimmedFile)
		return nil, fmt.Errorf("Trimmed clip is too small, trim may have failed")
	}

	return &DownloadResult{Path: trimmedFile, Ext: cobaltResult.Ext}, nil
}

func OrDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
