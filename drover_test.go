package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveExternalUI(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("mixed-port: 7890\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	uiDir := filepath.Join(dir, "ui")
	if err := os.MkdirAll(uiDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := resolveExternalUI(configPath, "ui"); got != uiDir {
		t.Errorf("resolveExternalUI(ui) = %q, want %q", got, uiDir)
	}
	if got := resolveExternalUI(configPath, "missing"); got != "" {
		t.Errorf("missing UI folder should not be overridden, got %q", got)
	}
	if got := resolveExternalUI(configPath, uiDir); got != "" {
		t.Errorf("absolute UI path needs no override, got %q", got)
	}
	if got := resolveExternalUI(configPath, "https://example.com/ui"); got != "" {
		t.Errorf("URL UI needs no override, got %q", got)
	}
	if got := resolveExternalUI(configPath, ""); got != "" {
		t.Errorf("empty UI should not be overridden, got %q", got)
	}
}
