package config

import (
	"path/filepath"
	"testing"
)

func TestConfiguredTempDirUsesEnvironment(t *testing.T) {
	expected := filepath.Join(t.TempDir(), "yoink-test")
	t.Setenv("YOINK_TEMP_DIR", expected)
	if actual := configuredTempDir(); actual != expected {
		t.Fatalf("configuredTempDir() = %q, want %q", actual, expected)
	}
}

func TestConfiguredTempDirUsesProductionDefault(t *testing.T) {
	t.Setenv("YOINK_TEMP_DIR", "")
	if actual := configuredTempDir(); actual != "/var/tmp/yoink" {
		t.Fatalf("configuredTempDir() = %q", actual)
	}
}
