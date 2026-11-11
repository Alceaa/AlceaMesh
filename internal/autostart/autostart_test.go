package autostart

import (
	"runtime"
	"testing"
)

func TestRegistryRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("autostart uses the Windows registry")
	}

	const testValueName = "AlceaMesh-verify-autostart"
	t.Cleanup(func() {
		_ = removeValue(testValueName)
	})
	_ = removeValue(testValueName)

	if _, ok, err := readValue(testValueName); err != nil || ok {
		t.Fatalf("before write: ok = %v, err = %v", ok, err)
	}

	command := Command(`C:\Program Files\AlceaMesh\alceamesh.exe`)
	if err := writeValue(testValueName, command); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, ok, err := readValue(testValueName)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !ok {
		t.Fatal("the value must be present after write")
	}
	if got != command {
		t.Errorf("command = %q, want %q", got, command)
	}

	if err := removeValue(testValueName); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok, err := readValue(testValueName); err != nil || ok {
		t.Errorf("after remove: ok = %v, err = %v", ok, err)
	}
	if err := removeValue(testValueName); err != nil {
		t.Errorf("removing a missing value must not fail: %v", err)
	}
}

func TestCommandQuotesThePath(t *testing.T) {
	command := Command(`C:\Program Files\AlceaMesh\alceamesh.exe`)
	want := `"C:\Program Files\AlceaMesh\alceamesh.exe" --tray`
	if command != want {
		t.Errorf("Command() = %q, want %q", command, want)
	}
}

func TestEnabledWithoutValue(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("autostart uses the Windows registry")
	}
	if !Enabled() {
		return
	}
	if _, ok, err := readValue(valueName); err != nil || !ok {
		t.Errorf("Enabled() = true but the value is unreadable: ok = %v, err = %v", ok, err)
	}
}
