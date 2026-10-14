package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/util"
)

type FailureRecord struct {
	Timestamp  time.Time `json:"timestamp"`
	DownloadID string    `json:"downloadId"`
	Stage      string    `json:"stage"`
	SourceHost string    `json:"sourceHost"`
	SourceHash string    `json:"sourceHash"`
	Error      string    `json:"error"`
	UserError  string    `json:"userError"`
	Kind       string    `json:"kind"`
	Repairable bool      `json:"repairable"`
}

// failureLogPath overrides the log location (tests). Empty means it follows
// config.TempDir, which is only final after config.Load has read .env.
var failureLogPath string

func failureLogFile() string {
	if failureLogPath != "" {
		return failureLogPath
	}
	return filepath.Join(config.TempDir, "state", "failures.jsonl")
}

var failureLogMu sync.Mutex
var loggedURLRe = regexp.MustCompile(`https?://\S+`)

// failureHost keeps only the site, so the failure log can say which site
// broke without recording what someone was downloading.
func failureHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

func classifyFailure(message string) (string, bool) {
	lower := strings.ToLower(message)
	sourceLimits := []string{
		"drm", "private video", "video is private", "deleted", "not available", "unavailable",
		"login required", "sign in", "confirm your age", "unsupported url", "no video formats",
	}
	for _, marker := range sourceLimits {
		if strings.Contains(lower, marker) {
			return "source_limit", false
		}
	}
	return "internal", true
}

func rotateFailureLog() error {
	info, err := os.Stat(failureLogFile())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() < 10*1024*1024 {
		return nil
	}
	_ = os.Remove(failureLogFile() + ".1")
	return os.Rename(failureLogFile(), failureLogFile()+".1")
}

func RecordDownloadFailure(downloadID, rawURL, stage string, failure error) error {
	if failure == nil {
		return nil
	}
	host := failureHost(rawURL)
	sum := sha256.Sum256([]byte(rawURL))
	errText := loggedURLRe.ReplaceAllString(failure.Error(), "[redacted-url]")
	kind, repairable := classifyFailure(errText)
	record := FailureRecord{
		Timestamp:  time.Now().UTC(),
		DownloadID: downloadID,
		Stage:      stage,
		SourceHost: host,
		SourceHash: hex.EncodeToString(sum[:]),
		Error:      errText,
		UserError:  util.ToUserError(errText),
		Kind:       kind,
		Repairable: repairable,
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}

	failureLogMu.Lock()
	defer failureLogMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(failureLogFile()), 0o700); err != nil {
		return err
	}
	if err := rotateFailureLog(); err != nil {
		return err
	}
	file, err := os.OpenFile(failureLogFile(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(file, "%s\n", data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	log.Printf("[FailureLog] job=%s kind=%s repairable=%t source=%s", shortJobID(downloadID), kind, repairable, host)
	return nil
}
