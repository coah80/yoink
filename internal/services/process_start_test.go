package services

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"testing"
)

func TestProcessStartSynchronizesReaders(t *testing.T) {
	for i := 0; i < 20; i++ {
		p := &ProcessInfo{}
		cmd := exec.Command("sh", "-c", "exit 0")
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.SignalProcess(syscall.Signal(0))
			if running := p.GetCmd(); running != nil && running.Process != nil {
				_ = running.Process.Signal(syscall.Signal(0))
			}
		}()
		if err := p.StartCmd(cmd); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		_ = cmd.Wait()
	}
}

func TestCancelledProcessDoesNotStart(t *testing.T) {
	p := &ProcessInfo{}
	p.SetCancelled(true)
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := p.StartCmd(cmd); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if cmd.Process != nil {
		t.Fatal("cancelled command started")
	}
}

func TestProcessOutputCapturesExitStderr(t *testing.T) {
	p := &ProcessInfo{}
	out, err := p.OutputCmd(exec.Command("sh", "-c", "printf output; printf failure >&2; exit 7"))
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || string(exitErr.Stderr) != "failure" || string(out) != "output" {
		t.Fatalf("output=%q err=%v", out, err)
	}
	if p.GetCmd().Process == nil {
		t.Fatal("missing process")
	}
}
