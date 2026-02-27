package util

import (
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	"github.com/coah80/yoink/internal/config"
)

var timeRe = regexp.MustCompile(`^(?:(\d{1,2}):)?(\d{1,2}):(\d{1,2}(?:\.\d+)?)$`)
var numRe = regexp.MustCompile(`^\d+(\.\d+)?$`)

type URLValidation struct {
	Valid bool
	Error string
}

func ValidateURL(rawURL string) URLValidation {
	if rawURL == "" {
		return URLValidation{false, "URL is required"}
	}
	if len(rawURL) > config.MaxURLLength {
		return URLValidation{false, "URL is too long"}
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return URLValidation{false, "Invalid URL format"}
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return URLValidation{false, "Only HTTP/HTTPS URLs are allowed"}
	}

	hostname := strings.ToLower(parsed.Hostname())
	if isPrivateHost(hostname) {
		return URLValidation{false, "Private/local URLs are not allowed"}
	}

	return URLValidation{true, ""}
}

var privateNets []*net.IPNet

func init() {
	cidrs := []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"0.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fe80::/10",
		"fc00::/7",
	}
	for _, cidr := range cidrs {
		_, network, _ := net.ParseCIDR(cidr)
		privateNets = append(privateNets, network)
	}
}

func isPrivateIP(ip net.IP) bool {
	for _, network := range privateNets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func isPrivateHost(hostname string) bool {
	if hostname == "" || hostname == "localhost" {
		return true
	}

	ip := net.ParseIP(hostname)
	if ip == nil {
		ip = net.ParseIP(strings.Trim(hostname, "[]"))
	}

	if ip != nil {
		return isPrivateIP(ip)
	}

	ips, err := net.LookupIP(hostname)
	if err != nil {
		return true
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return true
		}
	}
	return false
}

func ValidateVideoFile(filePath string) bool {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v",
		"-show_entries", "stream=codec_type",
		"-of", "csv=p=0",
		filePath,
	)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "video")
}

func ValidateTimeParam(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if numRe.MatchString(value) {
		return value
	}

	if timeRe.MatchString(value) {
		return value
	}
	return ""
}
