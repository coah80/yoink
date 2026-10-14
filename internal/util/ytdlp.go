package util

import (
	"net/url"
	"os"
	"strconv"
	"strings"
)

const bilibiliUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/140.0.0.0 Safari/537.36"

func YtdlpBinary() string {
	if bin := os.Getenv("YTDLP_BIN"); bin != "" {
		return bin
	}
	return "yt-dlp"
}

func IsYouTubeURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "youtu.be" || host == "youtube.com" || strings.HasSuffix(host, ".youtube.com")
}

func IsBilibiliURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "bilibili.com" || strings.HasSuffix(host, ".bilibili.com")
}

func YtdlpArgsForURL(rawURL string) []string {
	var args []string
	if IsYouTubeURL(rawURL) {
		args = append(args, GetYouTubeAuthArgs()...)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return args
	}
	host := strings.ToLower(parsed.Hostname())
	if IsBilibiliURL(rawURL) {
		args = append(args,
			"--user-agent", bilibiliUserAgent,
			"--referer", "https://www.bilibili.com/",
		)
	}
	if host == "tumblr.com" || strings.HasSuffix(host, ".tumblr.com") {
		args = append(args, "--force-generic-extractor")
	}
	if host == "giphy.com" || strings.HasSuffix(host, ".giphy.com") {
		args = append(args, "--add-headers", "Accept:*/*")
	}
	pathParts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(pathParts) == 2 && strings.HasPrefix(pathParts[0], "@") {
		if _, err := strconv.ParseUint(pathParts[1], 10, 64); err == nil {
			args = append(args, "--force-generic-extractor")
		}
	}
	return args
}

func NormalizeYtdlpURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "threads.com" || host == "www.threads.com" || host == "threads.net" || host == "www.threads.net" {
		if strings.Contains(parsed.Path, "/post/") {
			parsed.Scheme = "https"
			parsed.Host = "www.threads.com"
			parsed.RawQuery = ""
			parsed.Fragment = ""
			parsed.Path = strings.TrimSuffix(parsed.Path, "/")
			if !strings.HasSuffix(parsed.Path, "/embed") {
				parsed.Path += "/embed"
			}
			return parsed.String()
		}
	}
	if host == "clapperapp.com" || host == "www.clapperapp.com" || host == "newsclapper.com" || host == "www.newsclapper.com" {
		if strings.Contains(parsed.Path, "/video/") {
			parsed.Scheme = "https"
			parsed.Host = "aw.clapperapp.com"
			parsed.RawQuery = ""
			parsed.Fragment = ""
			return parsed.String()
		}
	}
	if host != "vimeo.com" && host != "www.vimeo.com" {
		return rawURL
	}
	id := strings.Trim(parsed.Path, "/")
	if id == "" || strings.Contains(id, "/") {
		return rawURL
	}
	if _, err := strconv.ParseUint(id, 10, 64); err != nil {
		return rawURL
	}
	return "https://player.vimeo.com/video/" + id
}
