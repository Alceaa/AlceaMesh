//go:build !windows

package ipc

import (
	"os"
	"path/filepath"
)

func SocketDir() string {
	return filepath.Join("/run", "alceamesh")
}

func prepareSocketDir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
