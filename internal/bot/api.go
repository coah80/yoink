package bot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type apiClient struct {
	baseURL string
	secret  string
	client  *http.Client
}

type jobStatusResponse struct {
	Status         string      `json:"status"`
	Progress       float64     `json:"progress"`
	Message        string      `json:"message"`
	FileName       string      `json:"fileName"`
	FileSize       int64       `json:"fileSize"`
	DownloadToken  string      `json:"downloadToken"`
	Speed          string      `json:"speed"`
	ETA            string      `json:"eta"`
	TotalVideos    int         `json:"totalVideos"`
	VideosComp     int         `json:"videosCompleted"`
	FailedVideos   []failedVid `json:"failedVideos"`
	PlaylistInfo   interface{} `json:"playlistInfo"`
	Error          string      `json:"error"`
	OutputFilename string      `json:"outputFilename"`
}

type failedVid struct {
	Num    int    `json:"num"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type startJobResponse struct {
	JobID string `json:"jobId"`
	Error string `json:"error"`
}

func newAPIClient(baseURL, secret string) *apiClient {
	return &apiClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (a *apiClient) doJSON(method, path string, body interface{}) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, a.baseURL+path, reqBody)
	if err != nil {
		return nil, 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+a.secret)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

func parseJobResponse(data []byte, httpStatus int) (string, error) {
	var resp startJobResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}
	if httpStatus != 200 {
		if resp.Error != "" {
			return "", errors.New(resp.Error)
		}
		return "", fmt.Errorf("HTTP %d", httpStatus)
	}
	return resp.JobID, nil
}

func (a *apiClient) startDownload(rawURL, format, quality, container, audioFormat string, playlist bool) (string, error) {
	body := map[string]interface{}{
		"url":         rawURL,
		"format":      format,
		"quality":     quality,
		"container":   container,
		"audioFormat": audioFormat,
		"playlist":    playlist,
	}

	data, status, err := a.doJSON("POST", "/api/bot/download", body)
	if err != nil {
		return "", err
	}
	return parseJobResponse(data, status)
}

func (a *apiClient) startPlaylistDownload(rawURL, format, quality, container, audioFormat string) (string, error) {
	body := map[string]interface{}{
		"url":          rawURL,
		"format":       format,
		"quality":      quality,
		"container":    container,
		"audioFormat":  audioFormat,
		"audioBitrate": "320",
	}

	data, status, err := a.doJSON("POST", "/api/bot/download-playlist", body)
	if err != nil {
		return "", err
	}
	return parseJobResponse(data, status)
}

func (a *apiClient) startBotConvert(fileURL, format string) (string, error) {
	body := map[string]interface{}{
		"url":    fileURL,
		"format": format,
	}

	data, status, err := a.doJSON("POST", "/api/bot/convert", body)
	if err != nil {
		return "", err
	}
	return parseJobResponse(data, status)
}

func (a *apiClient) startBotCompress(fileURL string, targetMB int, preset string) (string, error) {
	body := map[string]interface{}{
		"url":        fileURL,
		"targetSize": fmt.Sprintf("%d", targetMB),
		"preset":     preset,
	}

	data, status, err := a.doJSON("POST", "/api/bot/compress", body)
	if err != nil {
		return "", err
	}
	return parseJobResponse(data, status)
}

func (a *apiClient) startBotCompressFromToken(downloadToken string, targetMB int) (string, error) {
	body := map[string]interface{}{
		"downloadToken": downloadToken,
		"targetSize":    fmt.Sprintf("%d", targetMB),
		"preset":        "fast",
	}

	data, status, err := a.doJSON("POST", "/api/bot/compress", body)
	if err != nil {
		return "", err
	}
	return parseJobResponse(data, status)
}

func (a *apiClient) checkStatus(jobID string) (*jobStatusResponse, error) {
	data, status, err := a.doJSON("GET", "/api/bot/status/"+jobID, nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("status check failed: HTTP %d", status)
	}
	var resp jobStatusResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse status: %w", err)
	}
	return &resp, nil
}

func (a *apiClient) downloadFile(token string) ([]byte, string, error) {
	req, err := http.NewRequest("GET", a.baseURL+"/api/bot/download/"+token, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.secret)

	dlClient := &http.Client{Timeout: 5 * time.Minute}
	resp, err := dlClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	filename := ""
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		_, params, parseErr := mime.ParseMediaType(cd)
		if parseErr == nil {
			if name, ok := params["filename"]; ok {
				filename = name
			}
		}
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDiscordFileSize+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > maxDiscordFileSize {
		return nil, "", fmt.Errorf("file exceeds Discord upload limit")
	}
	return data, filename, nil
}

func (a *apiClient) getDownloadURL(token string) string {
	return a.baseURL + "/api/download/" + token
}

func normalizeURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	replacements := map[string]string{
		"fxtwitter.com":  "x.com",
		"fixupx.com":     "x.com",
		"vxtwitter.com":  "x.com",
		"fixvx.com":      "x.com",
		"twitter.com":    "x.com",
	}
	if replacement, ok := replacements[u.Host]; ok {
		u.Host = replacement
	}
	return u.String()
}

func isPlaylistURL(rawURL string) bool {
	return strings.Contains(rawURL, "list=") || strings.Contains(rawURL, "/playlist")
}
