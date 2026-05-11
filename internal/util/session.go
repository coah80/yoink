package util

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coah80/yoink/internal/config"
)

var (
	sessionMu     sync.RWMutex
	cachedPOToken string
	cachedVisitor string

	// Set these before calling StartSessionTokenRefresh.
	OnSessionTokenFailed    func(details string)
	OnSessionTokenRecovered func()
)

var sessionClient = &http.Client{Timeout: 10 * time.Second}

type sessionTokenResponse struct {
	POToken     string `json:"potoken"`
	VisitorData string `json:"visitor_data"`
}

type bgutilTokenResponse struct {
	POToken        string `json:"poToken"`
	ContentBinding string `json:"contentBinding"`
	ExpiresAt      string `json:"expiresAt"`
}

// GetSessionTokenArgs returns yt-dlp extractor-args for YouTube session tokens,
// or nil if no valid tokens are cached.
func GetSessionTokenArgs() []string {
	sessionMu.RLock()
	po := cachedPOToken
	vis := cachedVisitor
	sessionMu.RUnlock()

	if po == "" || vis == "" {
		return nil
	}
	return []string{
		"--extractor-args",
		fmt.Sprintf("youtube:po_token=WEB+%s;visitor_data=%s", po, vis),
	}
}

// StartSessionTokenRefresh starts a background goroutine that periodically
// fetches fresh session tokens from the yt-session-generator.
func StartSessionTokenRefresh() {
	go func() {
		// Initial fetch with retries
		for attempt := 1; attempt <= 5; attempt++ {
			if refreshToken() {
				log.Printf("[Session] Initial token fetched successfully")
				break
			}
			delay := time.Duration(attempt*2) * time.Second
			log.Printf("[Session] Initial fetch attempt %d/5 failed, retrying in %s...", attempt, delay)
			time.Sleep(delay)
		}

		// Always start periodic refresh loop
		startRefreshLoop()
	}()
}

func startRefreshLoop() {
	ticker := time.NewTicker(config.SessionTokenRefresh)
	defer ticker.Stop()

	consecutiveFailures := 0
	alertSent := false

	for range ticker.C {
		if refreshToken() {
			if consecutiveFailures > 0 {
				log.Printf("[Session] Token refreshed successfully after %d failures", consecutiveFailures)
			}
			if alertSent {
				if OnSessionTokenRecovered != nil {
					OnSessionTokenRecovered()
				}
				alertSent = false
			}
			consecutiveFailures = 0
		} else {
			consecutiveFailures++
			log.Printf("[Session] Token refresh failed (consecutive: %d)", consecutiveFailures)
			if consecutiveFailures >= 3 && !alertSent {
				if OnSessionTokenFailed != nil {
					OnSessionTokenFailed(fmt.Sprintf("Token refresh failed %d consecutive times. Generator may be down at %s", consecutiveFailures, config.SessionGeneratorURL))
				}
				alertSent = true
			}
		}
	}
}

func refreshToken() bool {
	// Try bgutil-ytdlp-pot-provider format first (POST /get_pot)
	if ok := refreshViaBgutil(); ok {
		return true
	}

	// Fall back to yt-session-generator format (POST /update + GET /token)
	return refreshViaSessionGenerator()
}

// refreshViaBgutil tries the bgutil-ytdlp-pot-provider API (POST /get_pot).
func refreshViaBgutil() bool {
	potURL := config.SessionGeneratorURL + "/get_pot"
	resp, err := sessionClient.Post(potURL, "application/json", strings.NewReader("{}"))
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false
	}

	var bgResp bgutilTokenResponse
	if err := json.Unmarshal(body, &bgResp); err != nil {
		return false
	}

	if bgResp.POToken == "" {
		return false
	}

	// bgutil provides contentBinding which contains visitor data
	visitorData := bgResp.ContentBinding

	sessionMu.Lock()
	cachedPOToken = bgResp.POToken
	if visitorData != "" {
		cachedVisitor = visitorData
	}
	sessionMu.Unlock()

	log.Printf("[Session] Token refreshed via bgutil (po_token=%d chars, visitor=%d chars)", len(bgResp.POToken), len(visitorData))
	return true
}

// refreshViaSessionGenerator tries the yt-session-generator API (POST /update + GET /token).
func refreshViaSessionGenerator() bool {
	updateURL := config.SessionGeneratorURL + "/update"
	resp, err := sessionClient.Post(updateURL, "", nil)
	if err != nil {
		log.Printf("[Session] POST /update failed: %v", err)
	} else {
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Printf("[Session] POST /update returned %d", resp.StatusCode)
		}
	}

	tokenURL := config.SessionGeneratorURL + "/token"
	resp, err = sessionClient.Get(tokenURL)
	if err != nil {
		log.Printf("[Session] GET /token failed: %v", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Session] GET /token returned %d", resp.StatusCode)
		return false
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		log.Printf("[Session] Failed to read token response: %v", err)
		return false
	}

	var token sessionTokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		log.Printf("[Session] Failed to parse token JSON: %v", err)
		return false
	}

	if token.POToken == "" || token.VisitorData == "" {
		log.Printf("[Session] Token response missing fields (potoken=%d chars, visitor_data=%d chars)", len(token.POToken), len(token.VisitorData))
		return false
	}

	sessionMu.Lock()
	cachedPOToken = token.POToken
	cachedVisitor = token.VisitorData
	sessionMu.Unlock()

	log.Printf("[Session] Token refreshed (po_token=%d chars, visitor_data=%d chars)", len(token.POToken), len(token.VisitorData))
	return true
}
