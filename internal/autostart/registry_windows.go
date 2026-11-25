//go:build windows

package autostart

import (
	"encoding/binary"
	"errors"
	"time"

	"golang.org/x/sys/windows/registry"
)

const (
	keyPath           = `Software\Microsoft\Windows\CurrentVersion\Run`
	approvalPath      = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
	approvalStateOn   = 0x02
	approvalStateOff  = 0x03
	approvalBlobBytes = 12
	windowsEpochTicks = 116444736000000000
)

func readValue(name string) (string, bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	defer key.Close()

	value, _, err := key.GetStringValue(name)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return value, true, nil
}

func writeValue(name, command string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetStringValue(name, command)
}

func removeValue(name string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer key.Close()

	if err := key.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func disabledBySystem(name string) bool {
	value, ok, err := readApproval(name)
	if err != nil || !ok || len(value) == 0 {
		return false
	}
	return value[0] == approvalStateOff
}

func readApproval(name string) ([]byte, bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, approvalPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer key.Close()

	value, _, err := key.GetBinaryValue(name)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return value, true, nil
}

func writeApproval(name string, enabled bool) error {
	blob := make([]byte, approvalBlobBytes)
	if enabled {
		blob[0] = approvalStateOn
	} else {
		blob[0] = approvalStateOff
		binary.LittleEndian.PutUint64(blob[4:], filetimeNow())
	}

	key, _, err := registry.CreateKey(registry.CURRENT_USER, approvalPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	return key.SetBinaryValue(name, blob)
}

func removeApproval(name string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, approvalPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer key.Close()

	if err := key.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func filetimeNow() uint64 {
	return uint64(time.Now().UTC().UnixNano()/100) + windowsEpochTicks
}
