package util

import (
	"fmt"
	"math"

	"github.com/coah80/yoink/internal/config"
)

type ResolutionResult struct {
	Width      int
	Height     int
	NeedsScale bool
}

func SelectResolution(width, height, availableBitrateK int) ResolutionResult {
	resolutions := []struct {
		W, H       int
		MinBitrate int
	}{
		{1920, 1080, config.BitrateThresholds[1080]},
		{1280, 720, config.BitrateThresholds[720]},
		{854, 480, config.BitrateThresholds[480]},
		{640, 360, config.BitrateThresholds[360]},
	}

	for _, r := range resolutions {
		if width < r.W && height < r.H {
			continue
		}
		if availableBitrateK >= r.MinBitrate {
			return ResolutionResult{r.W, r.H, width > r.W}
		}
	}

	for _, r := range resolutions {
		if availableBitrateK >= r.MinBitrate {
			return ResolutionResult{r.W, r.H, width > r.W}
		}
	}

	return ResolutionResult{640, 360, width > 640}
}

func GetDenoiseFilter(denoise string, sourceHeight int, sourceBitrateMbps float64, presetDenoise string) string {
	if denoise == "none" || presetDenoise == "none" {
		return ""
	}
	if denoise != "auto" {
		return config.DenoiseFilters[denoise]
	}
	if presetDenoise != "auto" {
		return config.DenoiseFilters[presetDenoise]
	}

	expectedBitrate := map[int]float64{
		360: 1, 480: 1.5, 720: 3, 1080: 6, 1440: 12, 2160: 25,
	}

	closest := 360
	minDiff := math.MaxFloat64
	for h := range expectedBitrate {
		diff := math.Abs(float64(h - sourceHeight))
		if diff < minDiff {
			minDiff = diff
			closest = h
		}
	}

	if sourceBitrateMbps > expectedBitrate[closest]*2.5 {
		return config.DenoiseFilters["heavy"]
	} else if sourceBitrateMbps > expectedBitrate[closest]*1.5 {
		return config.DenoiseFilters["moderate"]
	}
	return config.DenoiseFilters["light"]
}

func GetDownscaleResolution(sourceWidth, sourceHeight int) int {
	if sourceWidth > 1920 || sourceHeight > 1080 {
		return 1920
	} else if sourceWidth >= 1920 || sourceHeight >= 1080 {
		return 1280
	} else if sourceWidth >= 1280 || sourceHeight >= 720 {
		return 854
	}
	return 0
}

func BuildVideoFilters(denoiseFilter string, scaleWidth, sourceWidth int) string {
	var filters []string
	if scaleWidth > 0 && scaleWidth < sourceWidth {
		filters = append(filters, fmt.Sprintf("scale=%d:-2:flags=lanczos", scaleWidth))
	}
	if denoiseFilter != "" {
		filters = append(filters, denoiseFilter)
	}
	if len(filters) == 0 {
		return ""
	}
	result := ""
	for i, f := range filters {
		if i > 0 {
			result += ","
		}
		result += f
	}
	return result
}

func CalculateTargetBitrate(targetMB float64, durationSec float64, audioBitrateK int) int {
	targetBytes := targetMB * 1024 * 1024 * 0.95
	audioBytes := float64(audioBitrateK*1000/8) * durationSec
	videoBytes := targetBytes - audioBytes
	return int(math.Floor((videoBytes * 8) / durationSec / 1000))
}

func FormatETA(seconds float64) string {
	if seconds <= 0 {
		return ""
	}
	mins := int(seconds) / 60
	secs := int(seconds) % 60
	if mins > 0 {
		return fmt.Sprintf("%dm %ds", mins, secs)
	}
	return fmt.Sprintf("%ds", secs)
}
