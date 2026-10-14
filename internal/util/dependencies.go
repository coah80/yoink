package util

import (
	"fmt"
	"os/exec"
)

var GalleryDlAvailable bool
var WhisperAvailable bool

func CheckDependencies() {
	deps := []struct {
		name     string
		required bool
	}{
		{"yt-dlp", true},
		{"ffmpeg", true},
		{"ffprobe", true},
		{"gallery-dl", false},
		{"python3", false},
	}

	for _, dep := range deps {
		path, err := exec.LookPath(dep.name)
		if err != nil {
			if dep.required {
				fmt.Printf("✗ %s not found (REQUIRED)\n", dep.name)
			} else {
				fmt.Printf("- %s not found (optional)\n", dep.name)
			}
		} else {
			fmt.Printf("✓ %s found: %s\n", dep.name, path)
			if dep.name == "gallery-dl" {
				GalleryDlAvailable = true
			}
		}
	}

	if _, err := exec.LookPath("python3"); err == nil {
		// scripts/transcribe.py runs local models with faster-whisper
		cmd := exec.Command("python3", "-c", "import faster_whisper")
		if err := cmd.Run(); err == nil {
			WhisperAvailable = true
			fmt.Println("✓ faster-whisper available")
		} else {
			fmt.Println("- faster-whisper not found (optional, needed for local transcription)")
		}
	}
}
