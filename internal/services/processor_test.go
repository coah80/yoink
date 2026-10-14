package services

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func openStreamFixture(t *testing.T) (*os.File, os.FileInfo) {
	t.Helper()
	path := t.TempDir() + "/media.bin"
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return file, info
}

func TestServeDownloadContentSupportsRangeResume(t *testing.T) {
	file, info := openStreamFixture(t)
	request := httptest.NewRequest(http.MethodGet, "/download", nil)
	request.Header.Set("Range", "bytes=4-")
	response := httptest.NewRecorder()
	complete, err := serveDownloadContent(response, request, file, info, "media.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !complete || response.Code != http.StatusPartialContent {
		t.Fatalf("complete=%t status=%d", complete, response.Code)
	}
	if actual := response.Header().Get("Accept-Ranges"); actual != "bytes" {
		t.Fatalf("Accept-Ranges = %q", actual)
	}
	if actual := response.Header().Get("Content-Range"); actual != "bytes 4-9/10" {
		t.Fatalf("Content-Range = %q", actual)
	}
	if actual := response.Body.String(); actual != "456789" {
		t.Fatalf("body = %q", actual)
	}
}

func TestServeDownloadContentKeepsIncompleteRange(t *testing.T) {
	file, info := openStreamFixture(t)
	request := httptest.NewRequest(http.MethodGet, "/download", nil)
	request.Header.Set("Range", "bytes=2-4")
	response := httptest.NewRecorder()
	complete, err := serveDownloadContent(response, request, file, info, "media.bin")
	if err != nil {
		t.Fatal(err)
	}
	if complete || response.Code != http.StatusPartialContent {
		t.Fatalf("complete=%t status=%d", complete, response.Code)
	}
	if actual := response.Body.String(); actual != "234" {
		t.Fatalf("body = %q", actual)
	}
}

func TestServeDownloadContentCompletesFullResponse(t *testing.T) {
	file, info := openStreamFixture(t)
	request := httptest.NewRequest(http.MethodGet, "/download", nil)
	response := httptest.NewRecorder()
	complete, err := serveDownloadContent(response, request, file, info, "media.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !complete || response.Code != http.StatusOK {
		t.Fatalf("complete=%t status=%d", complete, response.Code)
	}
	if actual := response.Body.String(); actual != "0123456789" {
		t.Fatalf("body = %q", actual)
	}
}
