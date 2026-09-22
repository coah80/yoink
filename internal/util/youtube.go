package util

import (
	"net/url"
	"regexp"
	"strings"
)

var ytVideoIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{11}$`)

func ExtractYouTubeVideoID(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}

	if parsed.Host == "youtu.be" || parsed.Host == "www.youtu.be" {
		id := strings.TrimPrefix(parsed.Path, "/")
		if idx := strings.Index(id, "/"); idx >= 0 {
			id = id[:idx]
		}
		if ytVideoIDRe.MatchString(id) {
			return id
		}
	}

	if v := parsed.Query().Get("v"); ytVideoIDRe.MatchString(v) {
		return v
	}

	parts := strings.Split(parsed.Path, "/")
	for _, p := range parts {
		if ytVideoIDRe.MatchString(p) {
			return p
		}
	}

	return ""
}

func NormalizeYouTubeURL(rawURL string) string {
	if !strings.Contains(rawURL, "youtube.com") && !strings.Contains(rawURL, "youtu.be") {
		return rawURL
	}
	if strings.Contains(rawURL, "/clip/") || strings.Contains(rawURL, "/playlist") {
		return rawURL
	}
	id := ExtractYouTubeVideoID(rawURL)
	if id == "" {
		return rawURL
	}
	out := "https://www.youtube.com/watch?v=" + id
	if parsed, err := url.Parse(rawURL); err == nil {
		if list := parsed.Query().Get("list"); list != "" {
			out += "&list=" + url.QueryEscape(list)
		}
	}
	return out
}
