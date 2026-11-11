package main

import (
	"os"
	"testing"
)

func TestTrayRequestedFlag(t *testing.T) {
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })

	os.Args = []string{"alceamesh.exe"}
	if trayRequested() {
		t.Error("without the flag the app must start normally")
	}
	os.Args = []string{"alceamesh.exe", "--tray"}
	if !trayRequested() {
		t.Error("--tray must be detected")
	}
	os.Args = []string{"alceamesh.exe", "--TRAY"}
	if !trayRequested() {
		t.Error("--tray must be detected regardless of case")
	}
}
