//go:build !windows

package pki

import "os"

func restrict(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}
