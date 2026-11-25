//go:build !windows

package identity

import "os"

func protect(seed []byte) ([]byte, string, error) {
	return append([]byte(nil), seed...), SchemePlain, nil
}

func unprotect(blob []byte) ([]byte, error) {
	return blob, nil
}

func restrict(path string) error {
	return os.Chmod(path, 0o600)
}
