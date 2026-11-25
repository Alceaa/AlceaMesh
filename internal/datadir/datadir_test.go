package datadir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForDirCreatesDataSubfolder(t *testing.T) {
	base := t.TempDir()
	dir, ok := ForDir(base)
	if !ok {
		t.Fatalf("ForDir(%q) must succeed in a writable folder", base)
	}
	if want := filepath.Join(base, subDir); dir != want {
		t.Errorf("dir = %q, want %q", dir, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("the data folder must exist: err = %v", err)
	}
}

func TestForDirRejectsUnwritableBase(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if dir, ok := ForDir(blocker); ok {
		t.Errorf("ForDir(%q) = %q, want failure: a file cannot hold a folder", blocker, dir)
	}
	if _, ok := ForDir(""); ok {
		t.Error("an empty base must be rejected")
	}
}

func TestResolveFollowsOverride(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom")
	t.Setenv(OverrideEnv, custom)
	if got := resolve(); got != custom {
		t.Errorf("resolve() = %q, want %q", got, custom)
	}
}

func TestResolveIgnoresRelativeOverride(t *testing.T) {
	t.Setenv(OverrideEnv, "relative-path")
	got := resolve()
	if !filepath.IsAbs(got) {
		t.Errorf("resolve() = %q, want an absolute path", got)
	}
	if got == "relative-path" {
		t.Error("a relative override must not be used as-is")
	}
}

func TestResolvePrefersFolderNextToTheExecutable(t *testing.T) {
	t.Setenv(OverrideEnv, "")
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot locate the test binary: %v", err)
	}
	want, ok := ForDir(filepath.Dir(exe))
	if !ok {
		t.Skip("the test binary folder is not writable, the fallback applies")
	}
	if got := resolve(); got != want {
		t.Errorf("resolve() = %q, want %q", got, want)
	}
}

func TestIsWritable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh")
	if !IsWritable(dir) {
		t.Fatalf("IsWritable(%q) = false, want true", dir)
	}

	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if IsWritable(blocker) {
		t.Error("a path that is a file must not count as writable")
	}
}

func TestLegacyDirIsAbsoluteOrEmpty(t *testing.T) {
	if legacy := LegacyDir(); legacy != "" && !filepath.IsAbs(legacy) {
		t.Errorf("LegacyDir() = %q, want an absolute path", legacy)
	}
}
