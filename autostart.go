package main

import (
	"os"
	"strings"

	"alceacore.ru/alceamesh/internal/autostart"
)

func autostartEnabled() bool {
	return autostart.Enabled()
}

func setAutostart(enabled bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return autostart.Set(enabled, exe)
}

func trayRequested() bool {
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(arg, "--tray") {
			return true
		}
	}
	return false
}
