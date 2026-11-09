package settings

import "testing"

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
