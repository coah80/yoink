package routes

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/services"
	"github.com/coah80/yoink/internal/util"
)

func streamStems(w http.ResponseWriter, r *http.Request, ctx context.Context, processInfo *services.ProcessInfo, downloadID, downloadedPath, rawURL, filename, mode string, recovery bool, encode services.ProcessVideoOpts) {
	out, err := produceStems(ctx, processInfo, downloadID, downloadedPath, filename, mode, encode)
	os.Remove(downloadedPath)
	if err == nil && processInfo.IsCancelled() {
		os.Remove(out.Path)
		err = fmt.Errorf("Download cancelled")
	}
	if err != nil {
		handleDownloadError(w, downloadID, encode.AudioFormat, err)
		return
	}

	if recovery {
		if err := services.Global.MarkPendingJobReady(downloadID, out.Path, out.Ext); err != nil {
			handleDownloadError(w, downloadID, out.Ext, err)
			return
		}
		downloadURL := "/api/recovered/" + downloadID
		p := float64(100)
		services.Global.SendProgress(downloadID, "ready", "Recovered download is ready...", &p, map[string]interface{}{
			"downloadUrl": downloadURL,
			"filename":    out.Filename,
			"ext":         out.Ext,
		})
		services.Global.ReleaseJobKeepingPending(downloadID)
		respondJSON(w, 200, map[string]interface{}{"ready": true, "downloadUrl": downloadURL})
		return
	}

	services.StreamFile(w, r, out.Path, out.Filename, out.Ext, out.MimeType, downloadID, rawURL, "download", nil)
}

type stemOutput struct {
	Path     string
	Ext      string
	Filename string
	MimeType string
}

func produceStems(ctx context.Context, processInfo *services.ProcessInfo, downloadID, sourcePath, filename, mode string, encode services.ProcessVideoOpts) (*stemOutput, error) {
	stemDir := filepath.Join(config.TempDirs["download"], downloadID+"-stems")
	defer os.RemoveAll(stemDir)

	services.Global.UpdatePendingJob(downloadID, 100, "processing")
	stems, err := services.SplitStems(ctx, processInfo, sourcePath, stemDir, func(percent float64, message string) {
		services.Global.SendProgress(downloadID, "processing", message, &percent, nil)
	})
	if err != nil {
		return nil, err
	}

	wanted := map[string]string{"vocals": stems.Vocals, "instrumental": stems.Instrumental}
	if mode != "both" {
		wanted = map[string]string{mode: wanted[mode]}
	}

	p := float64(100)
	services.Global.SendProgress(downloadID, "processing", "Encoding stems...", &p, nil)
	encoded := make(map[string]string, len(wanted))
	for name, wav := range wanted {
		out := filepath.Join(stemDir, fmt.Sprintf("%s.%s", name, encode.AudioFormat))
		if _, err := services.ProcessVideo(wav, out, encode); err != nil {
			return nil, err
		}
		encoded[name] = out
	}

	base := orDefault(filename, "download")
	if mode != "both" {
		final := filepath.Join(config.TempDirs["download"], fmt.Sprintf("%s-final-%s.%s", downloadID, mode, encode.AudioFormat))
		if err := os.Rename(encoded[mode], final); err != nil {
			return nil, err
		}
		return &stemOutput{
			Path:     final,
			Ext:      encode.AudioFormat,
			Filename: fmt.Sprintf("%s (%s)", base, mode),
			MimeType: services.GetMimeType(encode.AudioFormat, true, false),
		}, nil
	}

	entries := make(map[string]string, len(encoded))
	for name, path := range encoded {
		entries[fmt.Sprintf("%s (%s).%s", util.SanitizeFilename(base), name, encode.AudioFormat)] = path
	}
	final := filepath.Join(config.TempDirs["download"], fmt.Sprintf("%s-final-stems.zip", downloadID))
	if err := services.ZipFiles(final, entries); err != nil {
		os.Remove(final)
		return nil, err
	}
	return &stemOutput{
		Path:     final,
		Ext:      "zip",
		Filename: base + " (stems)",
		MimeType: "application/zip",
	}, nil
}
