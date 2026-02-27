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
		cmd := exec.Command("python3", "-c", "import whisper")
		if err := cmd.Run(); err == nil {
			WhisperAvailable = true
			fmt.Println("✓ whisper module available")
		} else {
			fmt.Println("- whisper module not found (optional)")
		}
	}
}
