package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/coah80/yoink/internal/config"
)

type extractorEnvelope struct {
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"data"`
}

type TikTokExtractorData struct {
	Type      string   `json:"type"`
	FilePath  string   `json:"filePath"`
	FileSize  int64    `json:"fileSize"`
	Title     string   `json:"title"`
	Author    string   `json:"author"`
	Duration  int      `json:"duration"`
	AudioURL  string   `json:"audioUrl"`
	Thumbnail string   `json:"thumbnail"`
	Images    []string `json:"images"`
}

type TikTokMusicExtractorData struct {
	AudioURL  string `json:"audioUrl"`
	Title     string `json:"title"`
	Author    string `json:"author"`
	Duration  int    `json:"duration"`
	Thumbnail string `json:"thumbnail"`
}

type InstagramExtractorData struct {
	Type      string   `json:"type"`
	FilePath  string   `json:"filePath"`
	FileSize  int64    `json:"fileSize"`
	URLs      []string `json:"urls"`
	Title     string   `json:"title"`
	Author    string   `json:"author"`
	Thumbnail string   `json:"thumbnail"`
}

func ExtractorAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", config.ExtractorURL+"/health", nil)
	if err != nil {
		return false
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func callExtractor(ctx context.Context, path, rawURL string) (json.RawMessage, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()

	fullURL := fmt.Sprintf("%s%s?url=%s", config.ExtractorURL, path, url.QueryEscape(rawURL))
	req, err := http.NewRequestWithContext(reqCtx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("extractor request setup failed: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("extractor request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read extractor response: %w", err)
	}

	var envelope extractorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse extractor response: %w", err)
	}

	if !envelope.Success {
		return nil, fmt.Errorf("extractor error: %s", envelope.Error)
	}

	return envelope.Data, nil
}

func ExtractTikTok(ctx context.Context, rawURL string) (*TikTokExtractorData, error) {
	log.Printf("[Extractor] TikTok video: %s", rawURL)
	data, err := callExtractor(ctx, "/extract/tiktok", rawURL)
	if err != nil {
		return nil, err
	}

	var result TikTokExtractorData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse TikTok extractor data: %w", err)
	}
	return &result, nil
}

func ExtractTikTokMusic(ctx context.Context, rawURL string) (*TikTokMusicExtractorData, error) {
	log.Printf("[Extractor] TikTok music: %s", rawURL)
	data, err := callExtractor(ctx, "/extract/tiktok/music", rawURL)
	if err != nil {
		return nil, err
	}

	var result TikTokMusicExtractorData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse TikTok music extractor data: %w", err)
	}
	return &result, nil
}

func ExtractInstagram(ctx context.Context, rawURL string) (*InstagramExtractorData, error) {
	log.Printf("[Extractor] Instagram: %s", rawURL)
	data, err := callExtractor(ctx, "/extract/instagram", rawURL)
	if err != nil {
		return nil, err
	}

	var result InstagramExtractorData
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse Instagram extractor data: %w", err)
	}
	return &result, nil
}
