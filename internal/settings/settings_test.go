package settings

import (
	"os"
	"path/filepath"
	"testing"

	"alceacore.ru/alceamesh/internal/datadir"
)

func TestLoadMissingReturnsEmpty(t *testing.T) {
	settings, err := LoadFrom(t.TempDir())
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if *settings != (Settings{}) {
		t.Errorf("settings = %+v, want empty", settings)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := &Settings{
		DeviceName:  "Alice laptop",
		TunnelName:  "alcea",
		ConfigText:  "[Interface]\nPrivateKey = x\n",
		AcceptFiles: true,
	}
	if err := want.SaveTo(dir); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	got, err := LoadFrom(dir)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if *got != *want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSaveOverwrites(t *testing.T) {
	dir := t.TempDir()
	first := &Settings{DeviceName: "a", AcceptFiles: true}
	if err := first.SaveTo(dir); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	second := &Settings{DeviceName: "b", TunnelName: "t"}
	if err := second.SaveTo(dir); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	got, err := LoadFrom(dir)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got.DeviceName != "b" || got.TunnelName != "t" || got.AcceptFiles {
		t.Errorf("got %+v, want b/t/accept=false", got)
	}
}

func TestLoadMigratesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	legacy := t.TempDir()
	old := &Settings{DeviceName: "старый ПК", ConfigText: "[Interface]\nPrivateKey = x\n", AcceptFiles: true}
	if err := old.SaveTo(legacy); err != nil {
		t.Fatalf("SaveTo(legacy): %v", err)
	}

	got, err := loadMigrating(dir, legacy, true)
	if err != nil {
		t.Fatalf("loadMigrating: %v", err)
	}
	if got.DeviceName != old.DeviceName || got.ConfigText != old.ConfigText {
		t.Errorf("got %+v, want %+v", got, old)
	}
	if _, err := os.Stat(filepath.Join(dir, fileName)); err != nil {
		t.Errorf("the settings must be copied into the new folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacy, fileName)); !os.IsNotExist(err) {
		t.Errorf("the legacy file must be removed after migration: %v", err)
	}
}

func TestLoadKeepsTheLegacyFileWhenNotPortable(t *testing.T) {
	dir := t.TempDir()
	legacy := t.TempDir()
	if err := (&Settings{DeviceName: "старый ПК"}).SaveTo(legacy); err != nil {
		t.Fatalf("SaveTo(legacy): %v", err)
	}

	got, err := loadMigrating(dir, legacy, false)
	if err != nil {
		t.Fatalf("loadMigrating: %v", err)
	}
	if got.DeviceName != "старый ПК" {
		t.Errorf("DeviceName = %q, want the migrated value", got.DeviceName)
	}
	if _, err := os.Stat(filepath.Join(legacy, fileName)); err != nil {
		t.Errorf("a temporary data folder must not delete the main settings: %v", err)
	}
}

func TestLoadPrefersTheNewFileOverLegacy(t *testing.T) {
	dir := t.TempDir()
	legacy := t.TempDir()
	if err := (&Settings{DeviceName: "legacy"}).SaveTo(legacy); err != nil {
		t.Fatalf("SaveTo(legacy): %v", err)
	}
	if err := (&Settings{DeviceName: "current"}).SaveTo(dir); err != nil {
		t.Fatalf("SaveTo(dir): %v", err)
	}

	got, err := loadMigrating(dir, legacy, true)
	if err != nil {
		t.Fatalf("loadMigrating: %v", err)
	}
	if got.DeviceName != "current" {
		t.Errorf("DeviceName = %q, want current", got.DeviceName)
	}
	if _, err := os.Stat(filepath.Join(legacy, fileName)); err != nil {
		t.Errorf("the legacy file must be left alone: %v", err)
	}
}

func TestLoadWithoutLegacyFile(t *testing.T) {
	dir := t.TempDir()
	got, err := loadMigrating(dir, t.TempDir(), true)
	if err != nil {
		t.Fatalf("loadMigrating: %v", err)
	}
	if *got != (Settings{}) {
		t.Errorf("settings = %+v, want empty", got)
	}
}

func TestPortableOnlyNextToTheExecutable(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("cannot locate the test binary: %v", err)
	}
	if !portableTo(filepath.Join(filepath.Dir(exe), "data")) {
		t.Error("a data folder next to the executable must count as portable")
	}
	if portableTo(filepath.Join(t.TempDir(), "elsewhere")) {
		t.Error("an unrelated folder must not count as portable")
	}
}

func TestDirMatchesTheDataDir(t *testing.T) {
	if Dir() != datadir.Dir() {
		t.Errorf("Dir() = %q, want %q", Dir(), datadir.Dir())
	}
}
