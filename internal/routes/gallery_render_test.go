package routes

import (
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/coah80/yoink/internal/config"
	"github.com/coah80/yoink/internal/util"
)

func TestSlideshowWithAudioPreservesDuration(t *testing.T) {
	for name, sizes := range map[string][]image.Point{
		"landscape":     {{X: 128, Y: 72}, {X: 128, Y: 72}},
		"square":        {{X: 128, Y: 128}},
		"four_by_three": {{X: 128, Y: 96}},
		"mixed":         {{X: 128, Y: 72}, {X: 128, Y: 128}},
	} {
		t.Run(name, func(t *testing.T) { testSlideshowWithAudio(t, sizes) })
	}
}

func testSlideshowWithAudio(t *testing.T, sizes []image.Point) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	fixture := t.TempDir()
	for i, size := range sizes {
		shade := []color.RGBA{{R: 255, A: 255}, {B: 255, A: 255}}[i%2]
		img := image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
		for y := 0; y < size.Y; y++ {
			for x := 0; x < size.X; x++ {
				img.SetRGBA(x, y, shade)
			}
		}
		f, err := os.Create(filepath.Join(fixture, strconv.Itoa(i)+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", filepath.Join(fixture, "audio.wav")).CombinedOutput(); err != nil {
		t.Fatalf("audio fixture: %v %s", err, out)
	}
	bin := t.TempDir()
	script := "#!/bin/sh\nwhile [ $# -gt 0 ]; do if [ \"$1\" = -d ]; then shift; dest=$1; fi; shift; done\ncp \"$AUDIT_GALLERY_FIXTURE\"/* \"$dest\"/\n"
	if err := os.WriteFile(filepath.Join(bin, "gallery-dl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AUDIT_GALLERY_FIXTURE", fixture)
	previousDir, previousAvailable := config.TempDirs["gallery"], util.GalleryDlAvailable
	config.TempDirs["gallery"], util.GalleryDlAvailable = t.TempDir(), true
	t.Cleanup(func() {
		config.TempDirs["gallery"], util.GalleryDlAvailable = previousDir, previousAvailable
	})
	w := httptest.NewRecorder()
	handleSlideshow(w, httptest.NewRequest(http.MethodGet, "/api/gallery/slideshow?url=https://93.184.216.34/audit&progressId=audit-slideshow-local", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("slideshow HTTP%d: %s", w.Code, w.Body)
	}
	output := filepath.Join(t.TempDir(), "slideshow.mp4")
	if err := os.WriteFile(output, w.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=nw=1:nk=1", output).Output()
	if err != nil {
		t.Fatal(err)
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil || duration < 1.95 || duration > 2.15 {
		t.Fatalf("slideshow duration=%f, want full 2-second audio (err=%v)", duration, err)
	}
}
