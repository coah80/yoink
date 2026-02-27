package util

import (
	"os"
	"path/filepath"
	"strings"
)

var CookiesFile = filepath.Join(".", "cookies.txt")
var YouTubeCookiesFile = filepath.Join(".", "youtube-cookies.txt")

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
	return nil
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
