package services

import (
	"fmt"
	"strings"
)

type DownloadPreferences struct {
	Codec         string `json:"codec,omitempty"`
	H265          bool   `json:"h265,omitempty"`
	Metadata      *bool  `json:"metadata,omitempty"`
	FilenameStyle string `json:"filenameStyle,omitempty"`
	Uploader      string `json:"uploader,omitempty"`
}

func (p DownloadPreferences) EffectiveCodec(container string) string {
	if container == "webm" && p.Codec == "h264" {
		return "vp9"
	}
	return p.Codec
}

func (p DownloadPreferences) Validate() error {
	if p.Codec != "" && p.Codec != "h264" && p.Codec != "av1" && p.Codec != "vp9" {
		return fmt.Errorf("Invalid video codec")
	}
	if p.FilenameStyle != "" && p.FilenameStyle != "basic" && p.FilenameStyle != "classic" && p.FilenameStyle != "pretty" && p.FilenameStyle != "nerdy" {
		return fmt.Errorf("Invalid filename style")
	}
	return nil
}

func DownloadFilename(title, quality string, p DownloadPreferences) string {
	if title == "" {
		title = "download"
	}
	codec := p.Codec
	if codec == "" {
		codec = "h264"
	}
	switch p.FilenameStyle {
	case "classic":
		return fmt.Sprintf("%s (%s)", title, quality)
	case "pretty":
		if p.Uploader != "" {
			title += " - " + p.Uploader
		}
		return fmt.Sprintf("%s (%s, %s)", title, quality, codec)
	case "nerdy":
		audio := "aac"
		if codec == "av1" || codec == "vp9" {
			audio = "opus"
		}
		return fmt.Sprintf("%s_%s_%s_%s", strings.ReplaceAll(title, " ", "_"), quality, codec, audio)
	default:
		return title
	}
}

func videoPreferenceSelector(p DownloadPreferences, container string, height int) string {
	codec := map[string]string{"h264": "avc", "av1": "av01", "vp9": "vp9"}[p.EffectiveCodec(container)]
	if codec == "" {
		codec = "avc"
	}
	audio := "mp4a"
	if codec != "avc" {
		audio = "opus"
	}
	bounds := []string{""}
	if height > 0 {
		bounds = []string{fmt.Sprintf("[height<=%d]", height), fmt.Sprintf("[width<=%d]", height)}
	}
	var formats []string
	if p.H265 && codec == "avc" && container != "webm" {
		for _, bound := range bounds {
			formats = append(formats, fmt.Sprintf("bv[vcodec~='^(hev|hvc|h265)']%s+ba", bound))
		}
	}
	for _, bound := range bounds {
		formats = append(formats, fmt.Sprintf("bv[vcodec^=%s]%s+ba[acodec^=%s]", codec, bound, audio), fmt.Sprintf("b[vcodec^=%s]%s", codec, bound))
	}
	for _, bound := range bounds {
		formats = append(formats, "bv"+bound+"+ba", "b"+bound)
	}
	return strings.Join(append(formats, "b"), "/")
}
