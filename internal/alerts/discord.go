package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coah80/yoink/internal/config"
)

var (
	mu                sync.Mutex
	categoryCooldowns = make(map[string]time.Time)
)

const (
	colorBlue   = 0x3498DB
	colorOrange = 0xFFA500
	colorRed    = 0xFF4444
	colorCrit   = 0xFF0000
	colorGreen  = 0x2ECC71
)

type embed struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Color       int      `json:"color"`
	Fields      []field  `json:"fields,omitempty"`
	Timestamp   string   `json:"timestamp"`
	Footer      *footer  `json:"footer,omitempty"`
}

type field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type footer struct {
	Text string `json:"text"`
}

type payload struct {
	Content string  `json:"content,omitempty"`
	Embeds  []embed `json:"embeds"`
}

func send(category string, cooldown time.Duration, ping bool, color int, title, description string, fields map[string]string) {
	if !config.DiscordAlerts || config.DiscordWebhookURL == "" {
		return
	}

	mu.Lock()
	now := time.Now()
	if cooldown > 0 {
		if last, ok := categoryCooldowns[category]; ok && now.Sub(last) < cooldown {
			mu.Unlock()
			return
		}
	}
	categoryCooldowns[category] = now
	mu.Unlock()

	var embedFields []field
	for k, v := range fields {
		if v == "" {
			continue
		}
		if len(v) > 1024 {
			v = v[:1021] + "..."
		}
		embedFields = append(embedFields, field{Name: k, Value: v, Inline: true})
	}

	p := payload{
		Embeds: []embed{{
			Title:       title,
			Description: truncate(description, 2048),
			Color:       color,
			Fields:      embedFields,
			Timestamp:   now.UTC().Format(time.RFC3339),
			Footer:      &footer{Text: "yoink-go"},
		}},
	}

	if ping && config.DiscordPingUserID != "" {
		p.Content = fmt.Sprintf("<@%s>", config.DiscordPingUserID)
	}

	body, _ := json.Marshal(p)
	go func() {
		resp, err := http.Post(config.DiscordWebhookURL, "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("[Discord] send failed: %v", err)
			return
		}
		resp.Body.Close()
	}()
}

func ServerStarted() {
	send("server-start", 0, false, colorGreen, "Server Started", fmt.Sprintf("yoink-go %s listening on :%s", config.Version, config.Port), nil)
}

func ServerStopping() {
	send("server-stop", 0, false, colorOrange, "Server Stopping", "yoink-go is shutting down", nil)
}

func DownloadFailed(jobID, url string, err error) {
	send("download", 5*time.Second, true, colorRed, "Download Failed", err.Error(), map[string]string{
		"Job":   jobID,
		"URL":   truncate(url, 200),
		"Error": truncate(err.Error(), 500),
	})
}

func PlaylistFailed(jobID, url string, err error) {
	send("playlist", 5*time.Second, true, colorRed, "Playlist Failed", err.Error(), map[string]string{
		"Job":   jobID,
		"URL":   truncate(url, 200),
		"Error": truncate(err.Error(), 500),
	})
}

func ConversionFailed(jobID, format string, err error) {
	send("conversion", 5*time.Second, true, colorRed, "Conversion Failed", err.Error(), map[string]string{
		"Job":    jobID,
		"Format": format,
		"Error":  truncate(err.Error(), 500),
	})
}

func CompressionFailed(jobID string, err error) {
	send("compression", 5*time.Second, true, colorRed, "Compression Failed", err.Error(), map[string]string{
		"Job":   jobID,
		"Error": truncate(err.Error(), 500),
	})
}

func GalleryFailed(jobID, url string, err error) {
	send("gallery", 5*time.Second, true, colorRed, "Gallery Failed", err.Error(), map[string]string{
		"Job":   jobID,
		"URL":   truncate(url, 200),
		"Error": truncate(err.Error(), 500),
	})
}

func TranscriptionFailed(jobID string, err error) {
	send("transcription", 5*time.Second, true, colorRed, "Transcription Failed", err.Error(), map[string]string{
		"Job":   jobID,
		"Error": truncate(err.Error(), 500),
	})
}

func BotJobFailed(jobID, url string, err error) {
	send("bot", 5*time.Second, true, colorRed, "Bot Download Failed", err.Error(), map[string]string{
		"Job":   jobID,
		"URL":   truncate(url, 200),
		"Error": truncate(err.Error(), 500),
	})
}

func CookieIssue(details string) {
	send("cookie", 60*time.Second, true, colorOrange, "Cookie Issue", details, nil)
}

func CobaltAllFailed(jobID, url string, err error) {
	send("cobalt", 10*time.Second, false, colorOrange, "Cobalt All Instances Failed", err.Error(), map[string]string{
		"Job":   jobID,
		"URL":   truncate(url, 200),
		"Error": truncate(err.Error(), 500),
	})
}

func ProxyError(details string) {
	send("proxy", 60*time.Second, true, colorCrit, "Proxy Error", details, nil)
}

func SessionTokenFailed(details string) {
	send("session-token", 60*time.Second, true, colorOrange, "Session Token Failed", details, nil)
}

func SessionTokenRecovered() {
	send("session-token-recovered", 0, false, colorGreen, "Session Token Recovered", "YouTube session tokens are being refreshed successfully again.", nil)
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen-3] + "..."
	}
	return s
}
