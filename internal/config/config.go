package config

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

var Version = "dev"

var (
	Port    string
	EnvMode string

	BotSecret    string
	CobaltAPIKey string
	OpenAIAPIKey string

	ProxyHost       string
	ProxyPort       string
	ProxyUserPrefix string
	ProxyPassword   string
	ProxyCount      int

	DiscordWebhookURL string
	DiscordPingUserID string
	DiscordAlerts     bool

	SessionGeneratorURL  string
	SessionTokenRefresh  time.Duration
)

var JobLimits = map[string]int{
	"download":  6,
	"playlist":  2,
	"convert":   2,
	"compress":  1,
	"transcribe": 1,
	"fetchUrl":  2,
}

const (
	MaxQueueSize        = 50
	DiskSpaceMinGB      = 5
	FileSizeLimit       = 8 * 1024 * 1024 * 1024
	FileRetention       = 20 * time.Minute
	HeartbeatTimeout    = 30 * time.Second
	SessionIdleTimeout  = 60 * time.Second
	MaxPlaylistVideos   = 1000
	MaxVideoDuration    = 4 * 60 * 60
	MaxJobsPerClient    = 5
	RateLimitWindow     = 60 * time.Second
	RateLimitMax        = 60
	MaxURLLength        = 2048
	MaxSegments         = 20
	BotDownloadExpiry   = 5 * time.Minute
	PlaylistDownloadExp = 12 * time.Hour
	ChunkSize           = 50 * 1024 * 1024
	ChunkTimeout        = 30 * time.Minute
	AsyncJobTimeout     = 1 * time.Hour
)

var QualityHeight = map[string]int{
	"2160p": 2160,
	"1440p": 1440,
	"1080p": 1080,
	"720p":  720,
	"480p":  480,
	"360p":  360,
}

var ContainerMIMEs = map[string]string{
	"mp4":  "video/mp4",
	"webm": "video/webm",
	"mkv":  "video/x-matroska",
	"mov":  "video/quicktime",
}

var AudioMIMEs = map[string]string{
	"mp3":  "audio/mpeg",
	"m4a":  "audio/mp4",
	"opus": "audio/opus",
	"wav":  "audio/wav",
	"flac": "audio/flac",
}

const TempDir = "/var/tmp/yoink"

var TempDirs = map[string]string{
	"download":  filepath.Join(TempDir, "downloads"),
	"convert":   filepath.Join(TempDir, "convert"),
	"compress":  filepath.Join(TempDir, "compress"),
	"playlist":  filepath.Join(TempDir, "playlists"),
	"gallery":   filepath.Join(TempDir, "galleries"),
	"upload":    filepath.Join(TempDir, "uploads"),
	"bot":       filepath.Join(TempDir, "bot"),
	"transcribe": filepath.Join(TempDir, "transcribe"),
}

type PresetConfig struct {
	FFmpegPreset string
	CRF          map[string]int
	Denoise      string
	X264Params   string
}

var CompressionPresets = map[string]PresetConfig{
	"fast": {
		FFmpegPreset: "ultrafast",
		CRF:          map[string]int{"high": 26, "medium": 28, "low": 30},
		Denoise:      "none",
		X264Params:   "aq-mode=1",
	},
	"balanced": {
		FFmpegPreset: "medium",
		CRF:          map[string]int{"high": 22, "medium": 24, "low": 26},
		Denoise:      "auto",
		X264Params:   "aq-mode=3:aq-strength=0.9:psy-rd=1.0,0.0",
	},
	"quality": {
		FFmpegPreset: "slow",
		CRF:          map[string]int{"high": 20, "medium": 22, "low": 24},
		Denoise:      "auto",
		X264Params:   "aq-mode=3:aq-strength=0.9:psy-rd=1.0,0.0",
	},
}

var DenoiseFilters = map[string]string{
	"none":     "",
	"light":    "hqdn3d=2:1.5:3:2.25",
	"moderate": "hqdn3d=4:3:6:4.5",
	"heavy":    "hqdn3d=6:4:9:6",
}

var BitrateThresholds = map[int]int{
	1080: 2500,
	720:  1500,
	480:  800,
	360:  400,
}

var CobaltAPIs = []string{
	"https://nuko-c.meowing.de",
	"https://subito-c.meowing.de",
	"https://cessi-c.meowing.de",
}

var (
	AllowedFormats       = []string{"mp4", "webm", "mkv", "mov", "mp3", "m4a", "opus", "wav", "flac"}
	AllowedModes         = []string{"size", "quality"}
	AllowedQualities     = []string{"high", "medium", "low"}
	AllowedPresets       = []string{"fast", "balanced", "quality"}
	AllowedDenoise       = []string{"auto", "none", "light", "moderate", "heavy"}
	AllowedReencodes     = []string{"auto", "always", "never"}
	AllowedCropRatios    = []string{"16:9", "9:16", "1:1", "4:3", "4:5"}
	AllowedAudioBitrates = []string{"64", "96", "128", "192", "256", "320"}
)

var BotDetectionErrors = []string{
	"Sign in to confirm you",
	"confirm your age",
	"Sign in to confirm your age",
	"This video is unavailable",
	"Private video",
}

var HeavyJobTypes = []string{"playlist", "convert", "compress", "transcribe"}

func Load() {
	Port = envOrDefault("PORT", "3001")
	EnvMode = envOrDefault("NODE_ENV", "development")

	BotSecret = os.Getenv("BOT_SECRET")
	if BotSecret == "" {
		log.Println("[WARN] BOT_SECRET not set, bot endpoints will be unprotected")
	}

	CobaltAPIKey = os.Getenv("COBALT_API_KEY")
	OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")

	ProxyHost = os.Getenv("PROXY_HOST")
	ProxyPort = envOrDefault("PROXY_PORT", "80")
	ProxyUserPrefix = os.Getenv("PROXY_USER_PREFIX")
	ProxyPassword = os.Getenv("PROXY_PASSWORD")
	ProxyCount, _ = strconv.Atoi(envOrDefault("PROXY_COUNT", "0"))

	DiscordWebhookURL = os.Getenv("DISCORD_WEBHOOK_URL")
	DiscordPingUserID = os.Getenv("DISCORD_PING_USER_ID")
	DiscordAlerts = DiscordWebhookURL != ""

	SessionGeneratorURL = envOrDefault("SESSION_GENERATOR_URL", "http://localhost:8080")
	refreshMin, _ := strconv.Atoi(envOrDefault("SESSION_TOKEN_REFRESH_MIN", "15"))
	if refreshMin < 1 {
		refreshMin = 15
	}
	SessionTokenRefresh = time.Duration(refreshMin) * time.Minute
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func Contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
