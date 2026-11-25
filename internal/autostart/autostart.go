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
	return enabledFor(valueName)
}

func Set(enabled bool, exePath string) error {
	return set(valueName, enabled, exePath)
}

func enabledFor(name string) bool {
	command, ok, err := readValue(name)
	if err != nil || !ok || strings.TrimSpace(command) == "" {
		return false
	}
	return !disabledBySystem(name)
}

func set(name string, enabled bool, exePath string) error {
	if !enabled {
		err := removeValue(name)
		if approvalErr := removeApproval(name); err == nil {
			err = approvalErr
		}
		return err
	}
	if err := writeValue(name, Command(exePath)); err != nil {
		return err
	}
	return writeApproval(name, true)
}
