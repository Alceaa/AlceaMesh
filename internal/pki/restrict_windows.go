//go:build windows

package pki

import "os"

func restrict(string, os.FileMode) error {
	return nil
}
