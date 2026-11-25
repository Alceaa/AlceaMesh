package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"alceacore.ru/alceamesh/internal/datadir"
)

func TestWebviewDataDirUsesTheOverride(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "setup-cache")
	t.Setenv("ALCEAMESH_SETUP_DATA_DIR", custom)

	dir := webviewDataDir()
	if dir != custom {
		t.Fatalf("webviewDataDir() = %q, want %q", dir, custom)
	}
	if !datadir.IsWritable(dir) {
		t.Errorf("the folder must exist and be writable: %q", dir)
	}
}

func TestWebviewDataDirSkipsAnUnusableOverride(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("ALCEAMESH_SETUP_DATA_DIR", blocker)

	dir := webviewDataDir()
	if dir == blocker {
		t.Fatal("a path that is a file must be skipped")
	}
	if !datadir.IsWritable(dir) {
		t.Errorf("the fallback folder must be writable: %q", dir)
	}
}

func TestWebviewDataDirStaysInTheUserProfile(t *testing.T) {
	t.Setenv("ALCEAMESH_SETUP_DATA_DIR", "")

	dir := webviewDataDir()
	if dir == "" {
		t.Fatal("webviewDataDir() must not be empty")
	}
	t.Cleanup(func() {
		_ = os.Remove(dir)
	})

	lower := strings.ToLower(dir)
	local := strings.ToLower(strings.TrimSpace(os.Getenv("LocalAppData")))
	temp := strings.ToLower(strings.TrimSpace(os.TempDir()))

	if local != "" && strings.HasPrefix(lower, local) {
		return
	}
	if temp != "" && strings.HasPrefix(lower, temp) {
		return
	}
	t.Errorf("the installer cache must live in the user profile or temp, got %q", dir)
}
