package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestValidate(t *testing.T) {
	valid := []string{
		`C:\Program Files\AlceaMesh`,
		`D:\Apps\AlceaMesh`,
	}
	for _, dir := range valid {
		if err := Validate(dir); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", dir, err)
		}
	}

	invalid := []string{"", "   ", "AlceaMesh", `C:\`, `C:\Windows`, `C:\Windows\System32`, `C:\Program Files\Windows`}
	for _, dir := range invalid {
		if err := Validate(dir); err == nil {
			t.Errorf("Validate(%q) = nil, want an error", dir)
		}
	}
}

func TestPlanMentionsEveryChoice(t *testing.T) {
	plain := strings.Join(Plan(Options{Dir: `C:\Apps\AlceaMesh`}), "\n")
	if !strings.Contains(plain, `C:\Apps\AlceaMesh`) {
		t.Error("the plan must show the target folder")
	}
	if !strings.Contains(plain, "Автозапуск не включать") {
		t.Error("the plan must say that autostart stays off")
	}
	if strings.Contains(plain, "ярлык") || strings.Contains(plain, "Ярлык") {
		t.Error("shortcuts must not appear in the plan when they are disabled")
	}

	full := strings.Join(Plan(Options{
		Dir:       `C:\Apps\AlceaMesh`,
		Autostart: true,
		Shortcuts: true,
		Service:   true,
		Firewall:  true,
	}), "\n")
	for _, want := range []string{"Ярлык", "Запускать приложение", "службу", "брандмауэре"} {
		if !strings.Contains(full, want) {
			t.Errorf("the plan is missing %q", want)
		}
	}
}

func TestInstallDryRunChangesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "AlceaMesh")
	payload := fstest.MapFS{
		"alceamesh.exe":  {Data: []byte("app")},
		"alceameshd.exe": {Data: []byte("daemon")},
		"wintun.dll":     {Data: []byte("dll")},
	}

	var lines []string
	options := Options{
		Dir:       dir,
		Version:   "0.8.0",
		Service:   true,
		Firewall:  true,
		Shortcuts: true,
		Autostart: true,
		DryRun:    true,
	}
	if err := Install(payload, "", options, func(line string) {
		lines = append(lines, line)
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if len(lines) < len(PayloadFiles()) {
		t.Errorf("log has %d lines, expected at least %d", len(lines), len(PayloadFiles()))
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		t.Errorf("a dry run created %d entries in %s", len(entries), dir)
	}

	if err := Install(payload, "", Options{Dir: "not-absolute", DryRun: true}, nil); err == nil {
		t.Error("an invalid folder must be rejected even in a dry run")
	}
}

func TestInstallWithoutPayloadReports(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "AlceaMesh")
	err := Install(fstest.MapFS{}, "", Options{Dir: dir, Version: "0.8.0"}, nil)
	if err == nil {
		t.Fatal("install must fail when the payload is missing")
	}
	if !strings.Contains(err.Error(), "alceamesh.exe") {
		t.Errorf("error must name the missing file, got %v", err)
	}
}

func TestCopyFromFSCopiesContent(t *testing.T) {
	dir := t.TempDir()
	payload := fstest.MapFS{"alceamesh.exe": {Data: []byte("payload")}}
	target := filepath.Join(dir, "alceamesh.exe")

	if err := copyFromFS(payload, "alceamesh.exe", target); err != nil {
		t.Fatalf("copyFromFS: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "payload" {
		t.Errorf("content = %q, want %q", string(data), "payload")
	}
}

func TestRemoveProgramFilesKeepsRunningUninstaller(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "alceamesh-setup.exe")
	for _, name := range []string{"alceamesh-setup.exe", "alceamesh.exe", "wintun.dll"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := removeProgramFiles(dir, self, func(string, ...any) {}); err != nil {
		t.Fatalf("removeProgramFiles: %v", err)
	}
	if _, err := os.Stat(self); err != nil {
		t.Errorf("the running uninstaller must survive: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("%d entries left, want only the uninstaller", len(entries))
	}

	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "alceamesh.exe"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := removeProgramFiles(other, self, func(string, ...any) {}); err != nil {
		t.Fatalf("removeProgramFiles (outside): %v", err)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Errorf("the folder must be gone, stat err = %v", err)
	}
}

func TestUninstallRegistryValues(t *testing.T) {
	values := UninstallRegistryValues(`C:\Program Files\AlceaMesh`, "0.8.0-m8")
	if values["DisplayName"] != AppName {
		t.Errorf("DisplayName = %q", values["DisplayName"])
	}
	if values["DisplayVersion"] != "0.8.0-m8" {
		t.Errorf("DisplayVersion = %q", values["DisplayVersion"])
	}
	if !strings.Contains(values["UninstallString"], "alceamesh-setup.exe") ||
		!strings.Contains(values["UninstallString"], "--uninstall") {
		t.Errorf("UninstallString = %q", values["UninstallString"])
	}
	if values["InstallLocation"] != `C:\Program Files\AlceaMesh` {
		t.Errorf("InstallLocation = %q", values["InstallLocation"])
	}
}

func TestScriptsMentionExpectedPieces(t *testing.T) {
	firewall := FirewallScript(`C:\Program Files\AlceaMesh\alceamesh.exe`)
	for _, want := range []string{
		"New-NetFirewallRule",
		"AlceaMesh (alceamesh.exe, Inbound)",
		"AlceaMesh (alceamesh.exe, Outbound)",
		"Protocol UDP",
	} {
		if !strings.Contains(firewall, want) {
			t.Errorf("firewall script is missing %q", want)
		}
	}

	shortcuts := ShortcutScript(`C:\Apps\AlceaMesh\alceamesh.exe`, `C:\Apps\AlceaMesh`, Description)
	for _, want := range []string{"CreateShortcut", "CommonPrograms", "CommonDesktopDirectory", "IconLocation"} {
		if !strings.Contains(shortcuts, want) {
			t.Errorf("shortcut script is missing %q", want)
		}
	}

	if !strings.Contains(RemoveFirewallScript(), "AlceaMesh (*") {
		t.Error("the removal script must target every AlceaMesh rule")
	}
	if !strings.Contains(RemoveShortcutsScript(), "AlceaMesh.lnk") {
		t.Error("the removal script must delete the shortcuts")
	}
}

func TestQuoteEscapesSingleQuotes(t *testing.T) {
	if got := quote(`C:\O'Brien`); got != `C:\O''Brien` {
		t.Errorf("quote = %q", got)
	}
}
