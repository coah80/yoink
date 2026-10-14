package services

import (
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

func ConfigureYtdlpCancellation(cmd *exec.Cmd, grace time.Duration, pipes ...io.Closer) func() {
	var mu sync.Mutex
	var timer *time.Timer
	finished := false
	cmd.WaitDelay = grace
	cmd.Cancel = func() error {
		mu.Lock()
		defer mu.Unlock()
		if finished {
			return nil
		}
		err := cmd.Process.Signal(syscall.SIGTERM)
		if timer == nil {
			timer = time.AfterFunc(grace, func() {
				mu.Lock()
				defer mu.Unlock()
				if finished {
					return
				}
				_ = cmd.Process.Kill()
				for _, pipe := range pipes {
					_ = pipe.Close()
				}
			})
		}
		return err
	}
	return func() {
		mu.Lock()
		defer mu.Unlock()
		finished = true
		if timer != nil {
			timer.Stop()
		}
	}
}
