package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/coah80/yoink/internal/util"
)

type vimeoFormat struct {
	FormatID   string  `json:"format_id"`
	Protocol   string  `json:"protocol"`
	URL        string  `json:"url"`
	Vcodec     string  `json:"vcodec"`
	Acodec     string  `json:"acodec"`
	FormatNote string  `json:"format_note"`
	Height     int     `json:"height"`
	TBR        float64 `json:"tbr"`
	ABR        float64 `json:"abr"`
}

type vimeoMetadata struct {
	Formats []vimeoFormat `json:"formats"`
}

func isVimeoPlayerURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "player.vimeo.com") && strings.HasPrefix(parsed.Path, "/video/")
}

func vimeoHasVideo(format vimeoFormat) bool {
	if format.Vcodec != "" {
		return !strings.EqualFold(format.Vcodec, "none")
	}
	return strings.HasPrefix(strings.ToLower(format.Protocol), "http") && format.Height > 0
}

func vimeoHasAudio(format vimeoFormat) bool {
	if format.Acodec != "" {
		return !strings.EqualFold(format.Acodec, "none")
	}
	return strings.Contains(strings.ToLower(format.FormatID), "audio") || (strings.HasPrefix(strings.ToLower(format.Protocol), "http") && format.Height > 0)
}

func validateVimeoManifest(ctx context.Context, format vimeoFormat) error {
	if !strings.Contains(strings.ToLower(format.Protocol), "m3u8") {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, format.URL, nil)
	if err != nil {
		return fmt.Errorf("Vimeo manifest request failed")
	}
	req.Header.Set("Referer", "https://player.vimeo.com/")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Vimeo manifest request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Vimeo manifest request failed")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return fmt.Errorf("Vimeo manifest request failed")
	}
	manifest := strings.ToUpper(string(body))
	if strings.Contains(manifest, "METHOD=SAMPLE-AES") || strings.Contains(manifest, "COM.APPLE.STREAMINGKEYDELIVERY") {
		return fmt.Errorf("Vimeo video is DRM protected")
	}
	return nil
}

func vimeoCDNScore(format vimeoFormat) int {
	value := strings.ToLower(format.FormatID + " " + format.URL)
	if strings.Contains(value, "fastly") {
		return 2
	}
	if strings.Contains(value, "akfire") {
		return 1
	}
	return 0
}

func betterVimeoVideo(candidate, current vimeoFormat) bool {
	if candidate.Height != current.Height {
		return candidate.Height > current.Height
	}
	if vimeoCDNScore(candidate) != vimeoCDNScore(current) {
		return vimeoCDNScore(candidate) > vimeoCDNScore(current)
	}
	return candidate.TBR > current.TBR
}

func betterVimeoAudio(candidate, current vimeoFormat) bool {
	candidateHigh := strings.Contains(strings.ToLower(candidate.FormatNote+" "+candidate.FormatID), "high")
	currentHigh := strings.Contains(strings.ToLower(current.FormatNote+" "+current.FormatID), "high")
	if candidateHigh != currentHigh {
		return candidateHigh
	}
	if vimeoCDNScore(candidate) != vimeoCDNScore(current) {
		return vimeoCDNScore(candidate) > vimeoCDNScore(current)
	}
	return candidate.ABR > current.ABR
}

func selectVimeoFormats(formats []vimeoFormat, maxHeight int, audioOnly bool) (vimeoFormat, vimeoFormat, error) {
	var audio vimeoFormat
	for _, format := range formats {
		if format.URL == "" || !vimeoHasAudio(format) || vimeoHasVideo(format) {
			continue
		}
		if audio.URL == "" || betterVimeoAudio(format, audio) {
			audio = format
		}
	}
	if audioOnly {
		if audio.URL == "" {
			return vimeoFormat{}, vimeoFormat{}, fmt.Errorf("Vimeo audio stream not found")
		}
		return vimeoFormat{}, audio, nil
	}

	var combined vimeoFormat
	var video vimeoFormat
	for _, format := range formats {
		if format.URL == "" || !vimeoHasVideo(format) {
			continue
		}
		if maxHeight > 0 && (format.Height == 0 || format.Height > maxHeight) {
			continue
		}
		if vimeoHasAudio(format) {
			if combined.URL == "" || betterVimeoVideo(format, combined) {
				combined = format
			}
			continue
		}
		if video.URL == "" || betterVimeoVideo(format, video) {
			video = format
		}
	}
	if combined.URL != "" {
		return combined, vimeoFormat{}, nil
	}
	if video.URL == "" {
		return vimeoFormat{}, vimeoFormat{}, fmt.Errorf("Vimeo video stream not found at the requested quality")
	}
	if audio.URL == "" {
		return vimeoFormat{}, vimeoFormat{}, fmt.Errorf("Vimeo audio stream not found")
	}
	return video, audio, nil
}

func downloadVimeoFallback(ctx context.Context, rawURL, jobID string, maxHeight int, opts DownloadOpts) (*DownloadResult, error) {
	metadataArgs := append([]string{}, util.YtdlpArgsForURL(rawURL)...)
	metadataArgs = append(metadataArgs, "--dump-single-json", "--skip-download", "--no-playlist", rawURL)
	metadataCmd := exec.CommandContext(ctx, util.YtdlpBinary(), metadataArgs...)
	stopCancellation := ConfigureYtdlpCancellation(metadataCmd, 5*time.Second)
	defer stopCancellation()
	metadataOutput, err := metadataCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("Vimeo fallback metadata failed")
	}

	var metadata vimeoMetadata
	if err := json.Unmarshal(metadataOutput, &metadata); err != nil {
		return nil, fmt.Errorf("Vimeo fallback metadata was invalid")
	}
	video, audio, err := selectVimeoFormats(metadata.Formats, maxHeight, opts.IsAudio)
	if err != nil {
		return nil, err
	}
	if err := validateVimeoManifest(ctx, video); err != nil {
		return nil, err
	}
	if err := validateVimeoManifest(ctx, audio); err != nil {
		return nil, err
	}

	ext := "mp4"
	if opts.IsAudio {
		ext = "m4a"
	}
	outputPath := filepath.Join(opts.TempDir, fmt.Sprintf("%s%s.%s", opts.FilePrefix, jobID, ext))
	_ = os.Remove(outputPath)

	headers := "Referer: https://player.vimeo.com/\r\n"
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y"}
	if opts.IsAudio {
		args = append(args, "-headers", headers, "-i", audio.URL, "-map", "0:a:0", "-vn", "-c:a", "copy", "-movflags", "+faststart", outputPath)
	} else if audio.URL == "" {
		args = append(args, "-headers", headers, "-i", video.URL, "-map", "0:v:0", "-map", "0:a:0", "-c", "copy", "-movflags", "+faststart", outputPath)
	} else {
		args = append(args, "-headers", headers, "-i", video.URL, "-headers", headers, "-i", audio.URL, "-map", "0:v:0", "-map", "1:a:0", "-c", "copy", "-movflags", "+faststart", outputPath)
	}

	ffmpegCmd := exec.CommandContext(ctx, "/usr/bin/ffmpeg", args...)
	process := opts.ProcessInfo
	if process == nil {
		process = &ProcessInfo{}
	}
	if opts.OnCancel != nil {
		opts.OnCancel(func() {
			process.SetCancelled(true)
			process.KillProcess()
		})
	}
	if err := process.RunCmd(ffmpegCmd); err != nil {
		_ = os.Remove(outputPath)
		return nil, fmt.Errorf("Vimeo fallback download failed")
	}
	info, err := os.Stat(outputPath)
	if err != nil || info.Size() < 10000 {
		_ = os.Remove(outputPath)
		return nil, fmt.Errorf("Vimeo fallback output was incomplete")
	}
	if opts.OnProgress != nil {
		opts.OnProgress(100, "", "")
	}
	return &DownloadResult{Path: outputPath, Ext: ext}, nil
}
