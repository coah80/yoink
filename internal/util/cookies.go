package util

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var CookiesFile string
var YouTubeCookiesFile string

var (
	cookieRefreshMu   sync.Mutex
	lastCookieRefresh time.Time
	cookieRefreshMin  = 5 * time.Minute // minimum time between refresh attempts
)

// OnCookieRefreshNeeded is called when cookies need refreshing.
// Set this to alerts.CookieIssue or similar before use.
var OnCookieRefreshNeeded func(details string)

func InitCookiePaths() {
	CookiesFile = envOrDefault("COOKIES_FILE", filepath.Join(".", "cookies.txt"))
	YouTubeCookiesFile = envOrDefault("YOUTUBE_COOKIES_FILE", filepath.Join(".", "youtube-cookies.txt"))
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

var botDetectionErrors = []string{
	"Sign in to confirm you",
	"confirm your age",
	"Sign in to confirm your age",
	"This video is unavailable",
	"Private video",
}

func HasCookiesFile() bool {
	_, err := os.Stat(CookiesFile)
	return err == nil
}

func NeedsCookiesRetry(errorOutput string) bool {
	for _, e := range botDetectionErrors {
		if strings.Contains(errorOutput, e) {
			return true
		}
	}
	return false
}

func GetCookiesArgs() []string {
	if _, err := os.Stat(YouTubeCookiesFile); err == nil {
		return []string{"--cookies", YouTubeCookiesFile}
	}
	if _, err := os.Stat(CookiesFile); err == nil {
		return []string{"--cookies", CookiesFile}
	}
	return nil
}

// GetCookiePath returns the path to the active cookie file, or empty if none exists.
func GetCookiePath() string {
	if _, err := os.Stat(YouTubeCookiesFile); err == nil {
		return YouTubeCookiesFile
	}
	if _, err := os.Stat(CookiesFile); err == nil {
		return CookiesFile
	}
	return ""
}

// GetYouTubeAuthArgs returns yt-dlp args for YouTube authentication.
// Session tokens take priority; cookie file is included as supplement/fallback.
func GetYouTubeAuthArgs() []string {
	var args []string
	if tokenArgs := GetSessionTokenArgs(); tokenArgs != nil {
		args = append(args, tokenArgs...)
	}
	if cookieArgs := GetCookiesArgs(); cookieArgs != nil {
		args = append(args, cookieArgs...)
	}
	return args
}

// TriggerCookieRefresh attempts to refresh YouTube cookies via the configured
// cookie refresh script. It rate-limits to one attempt per cookieRefreshMin.
// This should be called when yt-dlp reports bot detection / auth failures.
func TriggerCookieRefresh(reason string) {
	cookieRefreshMu.Lock()
	if time.Since(lastCookieRefresh) < cookieRefreshMin {
		cookieRefreshMu.Unlock()
		log.Printf("[Cookies] Refresh skipped (last attempt %s ago)", time.Since(lastCookieRefresh).Round(time.Second))
		return
	}
	lastCookieRefresh = time.Now()
	cookieRefreshMu.Unlock()

	log.Printf("[Cookies] Refresh triggered: %s", reason)

	if OnCookieRefreshNeeded != nil {
		OnCookieRefreshNeeded("Cookie refresh triggered: " + reason)
	}

	scriptPath := os.Getenv("COOKIE_REFRESH_SCRIPT")
	if scriptPath == "" {
		scriptPath = filepath.Join(".", "refresh-cookies.sh")
	}

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		log.Printf("[Cookies] No refresh script at %s, skipping auto-refresh", scriptPath)
		return
	}

	go func() {
		log.Printf("[Cookies] Running refresh script: %s", scriptPath)
		cmd := exec.Command(scriptPath)
		cmd.Env = append(os.Environ(),
			"COOKIES_FILE="+CookiesFile,
			"YOUTUBE_COOKIES_FILE="+YouTubeCookiesFile,
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[Cookies] Refresh script failed: %v\n%s", err, string(output))
			return
		}
		log.Printf("[Cookies] Refresh script completed successfully")
	}()
}
