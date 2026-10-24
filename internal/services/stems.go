package services

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var AllowedStemModes = []string{"vocals", "instrumental", "both"}

var (
	StemsScript string
	StemsPython string
	StemsDevice string

	stemsThreads    = 4
	stemsGPUDuty    = 30
	stemsMemoryMax  = "5G"
	stemsCPUQuota   = "400%"
	stemsMaxMinutes = 60.0
	stemsMaxWaiting = int32(1)
	stemsTimeout    = 20 * time.Minute
	stemsSandboxed  bool
)

var (
	stemSlot    = make(chan struct{}, 1)
	stemPending atomic.Int32
)

func InitStems() {
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}

	StemsScript = os.Getenv("STEMS_SCRIPT")
	if StemsScript == "" {
		StemsScript = filepath.Join("scripts", "stems.py")
		if candidate := filepath.Join(exeDir, "scripts", "stems.py"); exeDir != "" && fileExists(candidate) {
			StemsScript = candidate
		}
	}

	StemsPython = os.Getenv("STEMS_PYTHON")
	if StemsPython == "" {
		StemsPython = "python3"
		if candidate := filepath.Join(exeDir, "stems-venv", "bin", "python"); exeDir != "" && fileExists(candidate) {
			StemsPython = candidate
		}
	}

	StemsDevice = envString("STEMS_DEVICE", "auto")
	stemsThreads = envInt("STEMS_THREADS", stemsThreads)
	stemsGPUDuty = min(100, envInt("STEMS_GPU_DUTY", stemsGPUDuty))
	stemsMemoryMax = envString("STEMS_MEMORY_MAX", stemsMemoryMax)
	stemsCPUQuota = envString("STEMS_CPU_QUOTA", stemsCPUQuota)
	stemsMaxMinutes = float64(envInt("STEMS_MAX_MINUTES", int(stemsMaxMinutes)))
	stemsMaxWaiting = int32(envInt("STEMS_MAX_WAITING", int(stemsMaxWaiting)))
	stemsTimeout = time.Duration(envInt("STEMS_TIMEOUT_MINUTES", int(stemsTimeout/time.Minute))) * time.Minute

	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		os.Setenv("XDG_RUNTIME_DIR", fmt.Sprintf("/run/user/%d", os.Getuid()))
	}
	stemsSandboxed = os.Getenv("STEMS_SANDBOX") != "false" &&
		exec.Command("systemd-run", "--user", "--scope", "--quiet", "--collect", "true").Run() == nil
}

func StemsAvailable() bool {
	return exec.Command(StemsPython, "-c", "import demucs, soundfile").Run() == nil
}

func StemsSandboxed() bool {
	return stemsSandboxed
}

func envString(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil && v > 0 {
		return v
	}
	return fallback
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// demucs gets its own cgroup when systemd is around, so a runaway split hits its own
// memory cap and gets killed instead of pushing the rest of the box into the oom killer
func stemsCommand(ctx context.Context, args ...string) *exec.Cmd {
	python := append([]string{StemsPython, "-I", StemsScript}, args...)
	var cmd *exec.Cmd
	if stemsSandboxed {
		wrapper := []string{"--user", "--scope", "--quiet", "--collect",
			"--nice=19",
			"-p", "MemoryMax=" + stemsMemoryMax,
			"-p", "MemorySwapMax=0",
			"-p", "CPUQuota=" + stemsCPUQuota,
			"-p", "IOWeight=10",
			"-p", fmt.Sprintf("RuntimeMaxSec=%d", int((stemsTimeout + time.Minute).Seconds())),
			"--",
		}
		cmd = exec.CommandContext(ctx, "systemd-run", append(wrapper, python...)...)
	} else {
		cmd = exec.CommandContext(ctx, "nice", append([]string{"-n", "19"}, python...)...)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	threads := strconv.Itoa(stemsThreads)
	cmd.Env = append(os.Environ(),
		"OMP_NUM_THREADS="+threads,
		"MKL_NUM_THREADS="+threads,
		"OPENBLAS_NUM_THREADS="+threads,
	)
	return cmd
}

func probeMinutes(path string) float64 {
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path).Output()
	if err != nil {
		return 0
	}
	seconds, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return seconds / 60
}

type StemPaths struct {
	Vocals       string
	Instrumental string
}

// ReserveStemJob caps how many stem jobs can be in flight (one splitting, the rest waiting)
// so a pile of them can't sit on every download slot
func ReserveStemJob() (func(), error) {
	if stemPending.Add(1) > stemsMaxWaiting+1 {
		stemPending.Add(-1)
		return nil, fmt.Errorf("stems: The stem splitter is busy right now, try again in a minute")
	}
	var once sync.Once
	return func() { once.Do(func() { stemPending.Add(-1) }) }, nil
}

func SplitStems(ctx context.Context, process *ProcessInfo, inputPath, outDir string, onProgress func(percent float64, message string)) (*StemPaths, error) {
	if minutes := probeMinutes(inputPath); minutes > stemsMaxMinutes {
		return nil, fmt.Errorf("stems: Vocal stems only work on audio up to %.0f minutes, this one is %.0f", stemsMaxMinutes, minutes)
	}

	onProgress(0, "Waiting for the stem splitter...")
	select {
	case stemSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-stemSlot }()

	runCtx, cancel := context.WithTimeout(ctx, stemsTimeout)
	defer cancel()
	cmd := stemsCommand(runCtx,
		"--input", inputPath,
		"--outdir", outDir,
		"--device", StemsDevice,
		"--threads", strconv.Itoa(stemsThreads),
		"--gpu-duty", fmt.Sprintf("%.2f", float64(stemsGPUDuty)/100),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := process.StartCmd(cmd); err != nil {
		return nil, fmt.Errorf("failed to start stem splitter: %w", err)
	}

	var tail []string
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			var update struct {
				Progress float64 `json:"progress"`
				Message  string  `json:"message"`
			}
			if json.Unmarshal([]byte(line), &update) == nil && update.Message != "" {
				onProgress(update.Progress, update.Message)
				continue
			}
			if line != "" {
				tail = append(tail[max(0, len(tail)-4):], line)
			}
		}
	}()

	out, _ := io.ReadAll(stdout)
	<-stderrDone
	waitErr := cmd.Wait()

	if process.IsCancelled() || ctx.Err() != nil {
		return nil, fmt.Errorf("Download cancelled")
	}
	if runCtx.Err() != nil {
		log.Printf("[stems] split of %s hit the %s timeout", inputPath, stemsTimeout)
		return nil, fmt.Errorf("stems: The stem split took too long and was stopped, try a shorter file")
	}

	var result struct {
		Success      bool   `json:"success"`
		Error        string `json:"error"`
		Vocals       string `json:"vocals"`
		Instrumental string `json:"instrumental"`
	}
	if err := json.Unmarshal(out, &result); err != nil || !result.Success {
		if strings.HasPrefix(result.Error, "The stem splitter") {
			return nil, fmt.Errorf("stems: %s", result.Error)
		}
		if result.Error != "" {
			log.Printf("[stems] split failed: %s", result.Error)
			return nil, fmt.Errorf("stems: The vocal stem split failed, try again")
		}
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) && exitErr.ExitCode() == -1 {
			log.Printf("[stems] splitter was killed, likely hit MemoryMax=%s", stemsMemoryMax)
			return nil, fmt.Errorf("stems: The stem splitter ran out of memory on this file, try a shorter one")
		}
		log.Printf("[stems] split failed: %v %s", waitErr, strings.Join(tail, " | "))
		return nil, fmt.Errorf("stems: The vocal stem split failed, try again")
	}
	return &StemPaths{Vocals: result.Vocals, Instrumental: result.Instrumental}, nil
}

func ZipFiles(zipPath string, entries map[string]string) error {
	out, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	for name, path := range entries {
		if err := addZipEntry(zw, name, path); err != nil {
			zw.Close()
			out.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func addZipEntry(zw *zip.Writer, name, path string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	entry, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: time.Now()})
	if err != nil {
		return err
	}
	_, err = io.Copy(entry, in)
	return err
}
