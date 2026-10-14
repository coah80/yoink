package util

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/coah80/yoink/internal/config"
)

var unsafeFilenameRe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
var multiSpaceRe = regexp.MustCompile(`\s+`)

func ClearTempDir() {
	for _, dir := range config.TempDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			os.MkdirAll(dir, 0755)
			continue
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			os.RemoveAll(p)
		}
	}
	fmt.Println("✓ Cleared temp directories")
}

func EnsureTempDirMounted() error {
	if runtime.GOOS != "linux" {
		return nil
	}
	dir, err := filepath.Abs(config.TempDir)
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return fmt.Errorf("cannot verify temp mount: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 4 && fields[4] == dir {
			return nil
		}
	}
	return fmt.Errorf("%s is not mounted; refusing to use unencrypted temp storage", dir)
}

func CleanupTempFiles(keep func(string, time.Time) bool, protectedPaths ...string) {
	now := time.Now()
	protected := make(map[string]struct{}, len(protectedPaths))
	for _, path := range protectedPaths {
		absolute, err := filepath.Abs(path)
		if err == nil {
			protected[filepath.Clean(absolute)] = struct{}{}
		}
	}
	for _, dir := range config.TempDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if keep != nil && keep(p, now) {
				continue
			}
			absolute, err := filepath.Abs(p)
			if err == nil {
				if _, ok := protected[filepath.Clean(absolute)]; ok {
					continue
				}
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if now.Sub(info.ModTime()) > config.FileRetention {
				os.RemoveAll(p)
				log.Printf("Cleaned up old temp: %s", e.Name())
			}
		}
	}

	if ds, err := GetDiskSpace(config.TempDir); err == nil {
		log.Printf("[DiskSpace] %.1fGB free / %.1fGB total (%.1fGB used)", ds.AvailGB, ds.TotalGB, ds.UsedGB)
		if ds.AvailGB < float64(config.DiskSpaceMinGB) {
			log.Printf("[DiskSpace] WARNING: Only %.1fGB free, below %dGB threshold!", ds.AvailGB, config.DiskSpaceMinGB)
		}
	}
}

func CleanupJobFiles(jobID string) {
	cleaned := 0
	for _, dir := range config.TempDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.Contains(e.Name(), jobID) {
				p := filepath.Join(dir, e.Name())
				if err := os.RemoveAll(p); err == nil {
					log.Printf("[Cleanup] Removed: %s", e.Name())
					cleaned++
				}
			}
		}
	}
	if cleaned == 0 {
		short := jobID
		if len(short) > 12 {
			short = short[:12]
		}
		log.Printf("[Cleanup] No files found for job %s", short)
	}
}

func SanitizeFilename(filename string) string {
	s := unsafeFilenameRe.ReplaceAllString(filename, "_")
	s = multiSpaceRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func StartCleanupInterval(keep func(string, time.Time) bool) {
	ticker := time.NewTicker(5 * time.Minute)
	go func() {
		for range ticker.C {
			CleanupTempFiles(keep)
		}
	}()
}
