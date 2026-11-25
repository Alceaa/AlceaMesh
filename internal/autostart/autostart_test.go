package autostart

import (
	"runtime"
	"testing"
)

const testValueName = "AlceaMesh-verify-autostart"

func cleanTestValue(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		_ = removeValue(testValueName)
		_ = removeApproval(testValueName)
	})
	_ = removeValue(testValueName)
	_ = removeApproval(testValueName)
}

func TestRegistryRoundTrip(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("autostart uses the Windows registry")
	}
	cleanTestValue(t)

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

func TestEnabledReflectsSystemFlag(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("autostart uses the Windows registry")
	}
	cleanTestValue(t)

	if err := set(testValueName, true, `C:\Apps\AlceaMesh\alceamesh.exe`); err != nil {
		t.Fatalf("set(true): %v", err)
	}
	if !enabledFor(testValueName) {
		t.Fatal("an entry just enabled must read back as enabled")
	}

	value, ok, err := readApproval(testValueName)
	if err != nil || !ok {
		t.Fatalf("the system flag must exist after set(true): ok = %v, err = %v", ok, err)
	}
	if len(value) != approvalBlobBytes || value[0] != approvalStateOn {
		t.Errorf("system flag = % x, want %d bytes starting with %#x", value, approvalBlobBytes, approvalStateOn)
	}

	if err := writeApproval(testValueName, false); err != nil {
		t.Fatalf("writeApproval(false): %v", err)
	}
	if enabledFor(testValueName) {
		t.Fatal("an entry disabled by Windows must read back as disabled")
	}

	if err := set(testValueName, true, `C:\Apps\AlceaMesh\alceamesh.exe`); err != nil {
		t.Fatalf("set(true) after a disable: %v", err)
	}
	if !enabledFor(testValueName) {
		t.Fatal("enabling again must clear the system flag")
	}

	if err := set(testValueName, false, ""); err != nil {
		t.Fatalf("set(false): %v", err)
	}
	if enabledFor(testValueName) {
		t.Fatal("after set(false) the entry must be gone")
	}
	if _, ok, err := readApproval(testValueName); err != nil || ok {
		t.Errorf("the system flag must be removed too: ok = %v, err = %v", ok, err)
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
	if disabledBySystem(valueName) {
		t.Error("Enabled() = true but Windows reports the entry as disabled")
	}
}
