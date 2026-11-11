//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const serviceName = "AlceaMesh"

func runHidden(name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		return text, fmt.Errorf("%v: %s", err, text)
	}
	return text, nil
}

func runElevated(name string, args ...string) error {
	executable, err := exec.LookPath(name)
	if err != nil {
		executable = name
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(executable)
	if err != nil {
		return err
	}
	parameters, err := windows.UTF16PtrFromString(strings.Join(args, " "))
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, parameters, nil, 1)
}

func serviceStart() (string, error) {
	return runHidden("sc.exe", "start", serviceName)
}

func serviceQuery() (string, error) {
	return runHidden("sc.exe", "query", serviceName)
}
