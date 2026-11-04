//go:build !windows

package main

import "fmt"

func isWindowsService() bool {
	return false
}

func runAsService() error {
	return fmt.Errorf("running as a service is only supported on Windows")
}

func cmdInstall(_ []string) error {
	return fmt.Errorf("service installation is only supported on Windows")
}

func cmdUninstall(_ []string) error {
	return fmt.Errorf("service installation is only supported on Windows")
}
