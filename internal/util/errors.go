package util

import "strings"

func ToUserError(message string) string {
	msg := strings.ToLower(message)

	if strings.Contains(msg, "cancelled") || strings.Contains(msg, "canceled") {
		return "Download cancelled"
	}
	if strings.Contains(msg, "content.video.unavailable") || strings.Contains(msg, "video unavailable") || strings.Contains(msg, "private video") || strings.Contains(msg, "this content is private") {
		return "This video is unavailable or has been removed"
	}
	if strings.Contains(msg, "content.video.live") || strings.Contains(msg, "live stream") {
		return "Live streams can't be downloaded yet"
	}
	if strings.Contains(msg, "content.video.age") || strings.Contains(msg, "age-restricted") || strings.Contains(msg, "age restricted") {
		return "This video is age-restricted"
	}
	if strings.Contains(msg, "content.too_long") || strings.Contains(msg, "too_long") {
		return "Video is too long (3+ hours)"
	}
	if strings.Contains(msg, "api.youtube.login") || strings.Contains(msg, "youtube.login") {
		return "YouTube requires login for this video"
	}
	if strings.Contains(msg, "api.rate_limited") {
		return "Rate limited, try again in a minute"
	}
	if strings.Contains(msg, "api.link.unsupported") {
		return "This link type isn't supported"
	}
	if strings.Contains(msg, "sign in to confirm") || strings.Contains(msg, "sign in to verify") {
		return "YouTube is blocking this request, try again later"
	}
	if strings.Contains(msg, "geo restricted") || strings.Contains(msg, "geo-restricted") || strings.Contains(msg, "not available in your country") {
		return "This video isn't available in the server's region"
	}
	if strings.Contains(msg, "copyright") {
		return "This video was removed for copyright"
	}
	if strings.Contains(msg, "members only") || strings.Contains(msg, "members-only") {
		return "This is a members-only video"
	}
	if strings.Contains(msg, "premium") {
		return "This video requires YouTube Premium"
	}
	if strings.Contains(msg, "http error 403") || strings.Contains(msg, "403 forbidden") {
		return "Access denied, the site is blocking downloads"
	}
	if strings.Contains(msg, "http error 404") || strings.Contains(msg, "404 not found") {
		return "Video not found, it may have been deleted"
	}
	if strings.Contains(msg, "unsupported url") {
		return "This website isn't supported"
	}
	if strings.Contains(msg, "no video formats") || strings.Contains(msg, "requested format not available") {
		return "No downloadable formats found"
	}
	if strings.Contains(msg, "rate") && !strings.Contains(msg, "format") {
		return "Rate limited, please wait and try again"
	}
	if strings.Contains(msg, "econnreset") || strings.Contains(msg, "fetch failed") || (strings.Contains(msg, "connection") && !strings.Contains(msg, "connected")) {
		return "Connection dropped, try again"
	}
	if strings.Contains(msg, "etimedout") || strings.Contains(msg, "timed out") || strings.Contains(msg, "timeout") {
		return "Connection timed out, try again"
	}
	if strings.Contains(msg, "enotfound") || strings.Contains(msg, "dns") {
		return "Couldn't reach the server, try again"
	}
	if strings.Contains(msg, "processing failed") || strings.Contains(msg, "encoding failed") {
		return "Processing failed"
	}
	if strings.Contains(msg, "download interrupted") {
		return "Download interrupted"
	}
	if strings.Contains(msg, "no videos were successfully downloaded") {
		return "No videos were successfully downloaded"
	}
	if strings.Contains(msg, "downloaded file not found") || strings.Contains(msg, "file not found") {
		return "Download failed"
	}
	if strings.Contains(msg, "playlist too large") {
		return message
	}
	if strings.Contains(msg, "too many active jobs") {
		return message
	}
	return "Download failed"
}
