package services

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestYtdlpCancellationAllowsCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	marker := filepath.Join(t.TempDir(), "cleaned")
	cmd := exec.CommandContext(ctx, "sh", "-c", `trap 'touch "$1"; exit 0' TERM; echo ready; while :; do sleep 0.1; done`, "sh", marker)
	stdout, _ := cmd.StdoutPipe()
	stop := ConfigureYtdlpCancellation(cmd, time.Second, stdout)
	defer stop()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make([]byte, 6)
	if _, err := io.ReadFull(stdout, ready); err != nil {
		t.Fatal(err)
	}
	cancel()
	_, _ = io.Copy(io.Discard, stdout)
	_ = cmd.Wait()
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("cleanup trap did not run: %v", err)
	}
}

func TestYtdlpCancellationClosesInheritedPipes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", `trap '' TERM; sleep 2 & echo ready; wait`)
	stdout, _ := cmd.StdoutPipe()
	stop := ConfigureYtdlpCancellation(cmd, 100*time.Millisecond, stdout)
	defer stop()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make([]byte, 6)
	if _, err := io.ReadFull(stdout, ready); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	cancel()
	_, _ = io.Copy(io.Discard, stdout)
	_ = cmd.Wait()
	if time.Since(start) > time.Second {
		t.Fatal("cancellation waited for inherited pipe holder")
	}
}

func TestKillProcessUsesCommandCancellation(t *testing.T) {
	called := false
	cmd := exec.Command("true")
	cmd.Process = &os.Process{}
	cmd.Cancel = func() error { called = true; return nil }
	p := &ProcessInfo{}
	p.SetCmd(cmd)
	p.KillProcess()
	if !called {
		t.Fatal("KillProcess bypassed command cleanup")
	}
}
