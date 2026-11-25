package datadir

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	OverrideEnv = "ALCEAMESH_DATA_DIR"
	subDir      = "data"
	appName     = "AlceaMesh"
)

var (
	once     sync.Once
	resolved string
)

func Dir() string {
	once.Do(func() {
		resolved = resolve()
	})
	return resolved
}

func resolve() string {
	if custom := strings.TrimSpace(os.Getenv(OverrideEnv)); custom != "" && filepath.IsAbs(custom) {
		if usable(custom) {
			return custom
		}
	}
	if exe, err := os.Executable(); err == nil {
		if dir, ok := ForDir(filepath.Dir(exe)); ok {
			return dir
		}
	}
	if base, err := os.UserCacheDir(); err == nil {
		fallback := filepath.Join(base, appName)
		if usable(fallback) {
			return fallback
		}
	}
	return filepath.Join(os.TempDir(), appName)
}

func ForDir(base string) (string, bool) {
	if strings.TrimSpace(base) == "" {
		return "", false
	}
	dir := filepath.Join(base, subDir)
	if !usable(dir) {
		return "", false
	}
	return dir, true
}

func LegacyDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, appName)
}

func usable(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	probe, err := os.CreateTemp(dir, ".write-probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return true
}
