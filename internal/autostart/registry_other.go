//go:build !windows

package autostart

import "errors"

func readValue(string) (string, bool, error) {
	return "", false, nil
}

func writeValue(string, string) error {
	return errors.New("autostart is not implemented on this platform yet")
}

func removeValue(string) error {
	return errors.New("autostart is not implemented on this platform yet")
}

func readApproval(string) ([]byte, bool, error) {
	return nil, false, nil
}

func writeApproval(string, bool) error {
	return errors.New("autostart is not implemented on this platform yet")
}

func removeApproval(string) error {
	return nil
}

func disabledBySystem(string) bool {
	return false
}
