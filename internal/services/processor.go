package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/util"
)

type ProcessVideoOpts struct {
	IsAudio      bool
	IsGif        bool
	AudioFormat  string
	AudioBitrate string
	Container    string
	JobID        string
}

type ProcessResult struct {
	Path    string
	Ext     string
	Skipped bool
}

func ProcessVideo(inputPath, outputPath string, opts ProcessVideoOpts) (*ProcessResult, error) {
	inputExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(inputPath), "."))
	outputExt := opts.Container
	if opts.IsGif {
		outputExt = "gif"
	} else if opts.IsAudio {
		outputExt = opts.AudioFormat
	}

	if !opts.IsAudio && !opts.IsGif && inputExt == outputExt {
		log.Printf("[%s] Format match (%s), skipping ffmpeg", opts.JobID, inputExt)
		return &ProcessResult{Path: inputPath, Ext: inputExt, Skipped: true}, nil
	}

	args := []string{"-y", "-i", inputPath}

	if opts.IsAudio {
		bitrate := opts.AudioBitrate
		if bitrate == "" {
			bitrate = "320"
		}
		switch opts.AudioFormat {
		case "mp3":
			args = append(args, "-codec:a", "libmp3lame", "-b:a", bitrate+"k")
		case "m4a":
			args = append(args, "-codec:a", "aac", "-b:a", bitrate+"k")
		case "opus":
			args = append(args, "-codec:a", "libopus", "-b:a", bitrate+"k")
		case "wav":
			args = append(args, "-codec:a", "pcm_s16le")
		case "flac":
			args = append(args, "-codec:a", "flac")
		default:
			args = append(args, "-codec:a", "copy")
		}
	} else if opts.IsGif {
		args = []string{"-y", "-i", inputPath,
			"-vf", "fps=15,scale=480:-1:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse",
			"-loop", "0",
		}
	} else {
		args = append(args, "-codec", "copy")
		if opts.Container == "mp4" || opts.Container == "mov" {
			args = append(args, "-movflags", "+faststart")
		}
	}

	args = append(args, outputPath)

	cmd := exec.Command("ffmpeg", args...)
	stderrPipe, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	stderrBytes, _ := io.ReadAll(stderrPipe)
	if err := cmd.Wait(); err != nil {
		errStr := string(stderrBytes)
		if len(errStr) > 500 {
			errStr = errStr[len(errStr)-500:]
		}
		log.Printf("[%s] FFmpeg failed. Last 500 chars: %s", opts.JobID, errStr)
		return nil, fmt.Errorf("Encoding failed (code %d)", cmd.ProcessState.ExitCode())
	}

	return &ProcessResult{Path: outputPath, Ext: outputExt, Skipped: false}, nil
}

func StreamFile(w http.ResponseWriter, r *http.Request, filePath string, filename, ext, mimeType, downloadID, sourceURL, jobType string, onCleanup func()) {
	info, err := os.Stat(filePath)
	if err != nil {
		http.Error(w, "File not found", 404)
		return
	}

	safeFilename := util.SanitizeFilename(filename)
	fullFilename := safeFilename + "." + ext
	asciiFilename := toASCII(safeFilename) + "." + ext

	Global.SendProgressSimple(downloadID, "sending", "Sending file to you...")

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
			asciiFilename, url.PathEscape(fullFilename)))

	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("[%s] Failed to open file for streaming: %v", downloadID, err)
		Global.SendProgressSimple(downloadID, "error", "Failed to send file")
		Global.ReleaseJob(downloadID)
		return
	}
	defer f.Close()

	_, copyErr := io.Copy(w, f)

	if copyErr != nil {
		log.Printf("[%s] Stream error: %v", downloadID, copyErr)
		Global.SendProgressSimple(downloadID, "error", "Failed to send file")
		Global.ReleaseJob(downloadID)
	} else {
		Global.SendProgressSimple(downloadID, "complete", "Download complete!")
		Global.UnregisterDownload(downloadID)
		Global.ReleaseJob(downloadID)
	}

	if onCleanup != nil {
		onCleanup()
	}

	go func() {
		util.CleanupJobFiles(downloadID)
	}()
}

func ProbeForGif(filePath string) bool {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams", "-show_format",
		filePath,
	)
	out, err := cmd.Output()
	if err != nil {
		return false
	}

	var probe struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}

	if err := json.Unmarshal(out, &probe); err != nil {
		return false
	}

	hasAudio := false
	for _, s := range probe.Streams {
		if s.CodecType == "audio" {
			hasAudio = true
			break
		}
	}

	duration := 999.0
	fmt.Sscanf(probe.Format.Duration, "%f", &duration)

	return !hasAudio && duration < 60
}

func GetMimeType(ext string, isAudio, isGif bool) string {
	if isGif {
		return "image/gif"
	}
	if isAudio {
		if mime, ok := config.AudioMIMEs[ext]; ok {
			return mime
		}
		return "audio/mpeg"
	}
	if mime, ok := config.ContainerMIMEs[ext]; ok {
		return mime
	}
	return "video/mp4"
}

func toASCII(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r <= 0x7E {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
