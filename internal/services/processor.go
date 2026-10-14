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
	DownloadPreferences
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

	codec := opts.EffectiveCodec(opts.Container)
	needsEncode := false
	if !opts.IsAudio && !opts.IsGif && codec != "" {
		probe := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_name", "-of", "default=nw=1:nk=1", inputPath)
		output, err := probe.Output()
		if err != nil {
			return nil, fmt.Errorf("Could not inspect video codec")
		}
		actual := strings.TrimSpace(string(output))
		needsEncode = actual != codec && !(actual == "hevc" && opts.H265 && opts.Container != "webm" && codec == "h264")
	}
	stripMetadata := opts.Metadata != nil && !*opts.Metadata
	if !opts.IsAudio && !opts.IsGif && inputExt == outputExt && !needsEncode && !stripMetadata {
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
	} else if needsEncode {
		args = append(args, "-map", "0:v:0", "-map", "0:a:0?")
		switch codec {
		case "av1":
			args = append(args, "-c:v", "libsvtav1", "-preset", "10", "-crf", "32", "-c:a", "libopus")
		case "vp9":
			args = append(args, "-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "6", "-crf", "32", "-b:v", "0", "-c:a", "libopus")
		default:
			args = append(args, "-c:v", "libx264", "-preset", "fast", "-crf", "23", "-c:a", "aac")
		}
	} else {
		args = append(args, "-codec", "copy")
		if opts.Container == "mp4" || opts.Container == "mov" {
			args = append(args, "-movflags", "+faststart")
		}
	}

	if stripMetadata {
		args = append(args, "-map_metadata", "-1")
	}
	args = append(args, outputPath)

	cmd := exec.Command("ffmpeg", args...)
	stderrPipe, _ := cmd.StderrPipe()

	process := Global.GetProcess(opts.JobID)
	if err := process.StartCmd(cmd); err != nil {
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
	Global.ReleaseJob(downloadID)

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
			asciiFilename, url.PathEscape(fullFilename)))

	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("[%s] Failed to open file for streaming: %v", downloadID, err)
		Global.SendProgressSimple(downloadID, "error", "Failed to send file")
		return
	}
	defer f.Close()

	complete, copyErr := serveDownloadContent(w, r, f, info, fullFilename)

	if copyErr != nil {
		log.Printf("[%s] Stream error: %v", downloadID, copyErr)
		if logErr := RecordDownloadFailure(downloadID, sourceURL, "sending", copyErr); logErr != nil {
			log.Printf("[%s] Failed to write structured failure log: %v", downloadID, logErr)
		}
		if err := Global.MarkPendingJobReady(downloadID, filePath, ext); err == nil {
			progress := float64(100)
			Global.SendProgress(downloadID, "ready", "Connection interrupted, retrying the finished download...", &progress, map[string]interface{}{
				"downloadUrl": "/api/recovered/" + downloadID,
				"filename":    filename,
				"ext":         ext,
			})
		} else {
			Global.SendProgressSimple(downloadID, "error", "Failed to send file")
		}
		Global.ReleaseJobKeepingPending(downloadID)
		return
	}
	if !complete {
		if err := Global.MarkPendingJobReady(downloadID, filePath, ext); err == nil {
			progress := float64(100)
			Global.SendProgress(downloadID, "ready", "Download ready to resume...", &progress, map[string]interface{}{
				"downloadUrl": "/api/recovered/" + downloadID,
				"filename":    filename,
				"ext":         ext,
			})
		}
		Global.ReleaseJobKeepingPending(downloadID)
		return
	}
	Global.SendProgressSimple(downloadID, "complete", "Download complete!")
	Global.UnregisterDownload(downloadID)

	if onCleanup != nil {
		onCleanup()
	} else {
		util.CleanupJobFiles(downloadID)
	}
	Global.ReleaseJob(downloadID)
}

type streamResponseWriter struct {
	http.ResponseWriter
	status       int
	bytesWritten int64
	writeErr     error
}

func (w *streamResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *streamResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	written, err := w.ResponseWriter.Write(data)
	w.bytesWritten += int64(written)
	if err != nil && w.writeErr == nil {
		w.writeErr = err
	}
	return written, err
}

func (w *streamResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	written, err := io.Copy(w.ResponseWriter, reader)
	w.bytesWritten += written
	if err != nil && w.writeErr == nil {
		w.writeErr = err
	}
	return written, err
}

func serveDownloadContent(w http.ResponseWriter, r *http.Request, file *os.File, info os.FileInfo, filename string) (bool, error) {
	tracked := &streamResponseWriter{ResponseWriter: w}
	http.ServeContent(tracked, r, filename, info.ModTime(), file)
	if tracked.writeErr != nil {
		return false, tracked.writeErr
	}
	if r.Method == http.MethodHead || tracked.status >= http.StatusBadRequest {
		return false, nil
	}
	contentRange := tracked.Header().Get("Content-Range")
	if contentRange == "" {
		return tracked.bytesWritten == info.Size(), nil
	}
	var start, end, total int64
	if _, err := fmt.Sscanf(contentRange, "bytes %d-%d/%d", &start, &end, &total); err != nil {
		return false, nil
	}
	expected := end - start + 1
	return total > 0 && end == total-1 && tracked.bytesWritten == expected, nil
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
