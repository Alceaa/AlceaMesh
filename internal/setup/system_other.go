//go:build !windows

package setup

import "errors"

var errUnsupported = errors.New("установка поддерживается только в Windows")

func Elevated() bool {
	return false
}

func Elevate(string) error {
	return errUnsupported
}

func RunPowerShell(string) (string, error) {
	return "", errUnsupported
}

func WriteUninstallRegistry(map[string]string) error {
	return errUnsupported
}

func DeleteUninstallRegistry() error {
	return errUnsupported
}

func SetAppAutostart(bool, string) error {
	return errUnsupported
}

func installService(string, func(string, ...any)) error {
	return errUnsupported
}

func removeService(string, func(string, ...any)) error {
	return errUnsupported
}

func stopRunningComponents(func(string, ...any)) bool {
	return false
}

func startApp(string) error {
	return errUnsupported
}
