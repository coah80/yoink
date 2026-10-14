package services

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordDownloadFailureWritesRedactedStructuredLog(t *testing.T) {
	originalPath := failureLogPath
	failureLogPath = filepath.Join(t.TempDir(), "failures.jsonl")
	t.Cleanup(func() {
		failureLogPath = originalPath
	})

	err := RecordDownloadFailure("failure-job", "https://www.youtube.com/watch?v=jNQXAC9IVRw&token=secret", "downloading", errors.New("Downloaded file not found"))
	if err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(failureLogPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("failure log is empty")
	}
	var record FailureRecord
	if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.DownloadID != "failure-job" || record.Stage != "downloading" || !record.Repairable {
		t.Fatalf("record = %#v", record)
	}
	if record.SourceHost != "www.youtube.com" {
		t.Fatalf("source host = %q", record.SourceHost)
	}
	if line := scanner.Text(); strings.Contains(line, "jNQXAC9IVRw") || strings.Contains(line, "secret") {
		t.Fatalf("failure log kept the URL: %s", line)
	}
}

func TestRecordDownloadFailureClassifiesDRMAsSourceLimit(t *testing.T) {
	originalPath := failureLogPath
	failureLogPath = filepath.Join(t.TempDir(), "failures.jsonl")
	t.Cleanup(func() {
		failureLogPath = originalPath
	})

	if err := RecordDownloadFailure("drm-job", "https://vimeo.com/100000002", "downloading", errors.New("This video is DRM protected")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(failureLogPath)
	if err != nil {
		t.Fatal(err)
	}
	var record FailureRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Repairable || record.Kind != "source_limit" {
		t.Fatalf("record = %#v", record)
	}
}
