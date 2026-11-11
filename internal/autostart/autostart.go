package autostart

import "strings"

const (
	valueName = "AlceaMesh"
	trayFlag  = "--tray"
)

func Command(exePath string) string {
	return `"` + exePath + `" ` + trayFlag
}

func Enabled() bool {
	command, ok, err := readValue(valueName)
	if err != nil || !ok {
		return false
	}
	return strings.TrimSpace(command) != ""
}

func Set(enabled bool, exePath string) error {
	if !enabled {
		return removeValue(valueName)
	}
	return writeValue(valueName, Command(exePath))
}
