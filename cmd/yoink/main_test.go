package main

import (
	"reflect"
	"testing"

	"github.com/coah80/yoink/internal/services"
)

func TestReadyPendingOutputPaths(t *testing.T) {
	jobs := []*services.PendingJob{
		{Ready: true, OutputPath: "/tmp/ready.mp4"},
		{Ready: false, OutputPath: "/tmp/running.mp4"},
		{Ready: true},
	}
	want := []string{"/tmp/ready.mp4"}
	if got := readyPendingOutputPaths(jobs); !reflect.DeepEqual(got, want) {
		t.Fatalf("ready output paths = %#v, want %#v", got, want)
	}
}
