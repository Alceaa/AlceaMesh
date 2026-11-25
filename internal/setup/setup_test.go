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
	if !strings.Contains(plain, "от прежних версий") {
		t.Error("the plan must mention cleaning up leftovers")
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

func TestPlanShowsThePreviousInstallation(t *testing.T) {
	previous := t.TempDir()
	for _, name := range []string{"alceamesh.exe", "alceameshd.exe"} {
		if err := os.WriteFile(filepath.Join(previous, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	plan := strings.Join(Plan(Options{Dir: `D:\Apps\AlceaMesh`, PreviousDir: previous}), "\n")
	if !removalLineMentions(plan, previous) {
		t.Errorf("the plan must mention removing %s: %s", previous, plan)
	}

	same := strings.Join(Plan(Options{Dir: previous, PreviousDir: previous}), "\n")
	if removalLineMentions(same, previous) {
		t.Errorf("reinstalling into the same folder must not promise a removal: %s", same)
	}

	untouched := t.TempDir()
	empty := strings.Join(Plan(Options{Dir: `D:\Apps\AlceaMesh`, PreviousDir: untouched}), "\n")
	if removalLineMentions(empty, untouched) {
		t.Errorf("a folder without our files must not be removed: %s", empty)
	}
}

func removalLineMentions(plan, dir string) bool {
	for _, line := range strings.Split(plan, "\n") {
		if strings.Contains(line, "прежнюю копию") && strings.Contains(line, dir) {
			return true
		}
	}
	return false
}

func TestRemovalCandidates(t *testing.T) {
	ours := t.TempDir()
	if err := os.WriteFile(filepath.Join(ours, "alceamesh.exe"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	empty := t.TempDir()
	target := `D:\Apps\AlceaMesh`

	got := removalCandidates(target, ours, empty, target)
	if len(got) != 1 || got[0] != ours {
		t.Errorf("removalCandidates() = %v, want only %q", got, ours)
	}

	if got := removalCandidates(target, empty); len(got) != 0 {
		t.Errorf("removalCandidates() = %v, want nothing", got)
	}
}

func TestPreviousInstallGuards(t *testing.T) {
	ours := t.TempDir()
	if err := os.WriteFile(filepath.Join(ours, "alceamesh.exe"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := previousInstall(ours, `D:\Apps\AlceaMesh`); got != ours {
		t.Errorf("previousInstall() = %q, want %q", got, ours)
	}
	if got := previousInstall(ours, ours); got != "" {
		t.Errorf("the same folder must be skipped, got %q", got)
	}
	if got := previousInstall(ours, filepath.Join(ours, "nested")); got != "" {
		t.Errorf("a parent of the new folder must never be removed, got %q", got)
	}
	if got := previousInstall("  ", ours); got != "" {
		t.Errorf("an empty previous folder must be skipped, got %q", got)
	}
	if got := previousInstall(t.TempDir(), ours); got != "" {
		t.Errorf("a folder without our files must be skipped, got %q", got)
	}
	if got := previousInstall(`C:\`, ours); got != "" {
		t.Errorf("a drive root must never be removed, got %q", got)
	}
}

func TestDirFromExecutable(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "alceamesh-setup.exe")
	if err := os.WriteFile(filepath.Join(dir, "alceamesh.exe"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := DirFromExecutable(self); got != dir {
		t.Errorf("DirFromExecutable() = %q, want %q", got, dir)
	}
	if got := DirFromExecutable(""); got != "" {
		t.Errorf("DirFromExecutable(\"\") = %q, want empty", got)
	}
	if got := DirFromExecutable(filepath.Join(t.TempDir(), uninstallExe)); got != "" {
		t.Errorf("a folder without our files must be rejected, got %q", got)
	}
}

func TestDataDirsCoverPortableAndSystemPlaces(t *testing.T) {
	t.Setenv("AppData", `C:\Users\test\AppData\Roaming`)
	t.Setenv("LocalAppData", `C:\Users\test\AppData\Local`)

	dirs := dataDirs(`D:\Apps\AlceaMesh`)
	want := []string{
		`D:\Apps\AlceaMesh\data`,
		filepath.Join(`C:\Users\test\AppData\Roaming`, AppName),
		filepath.Join(`C:\Users\test\AppData\Local`, AppName),
	}
	if len(dirs) != len(want) {
		t.Fatalf("dataDirs() = %v, want %v", dirs, want)
	}
	for index := range want {
		if dirs[index] != want[index] {
			t.Errorf("dataDirs()[%d] = %q, want %q", index, dirs[index], want[index])
		}
	}

	t.Setenv("AppData", "")
	t.Setenv("LocalAppData", "")
	if got := dataDirs(`D:\Apps\AlceaMesh`); len(got) != 1 {
		t.Errorf("dataDirs() without a profile = %v, want only the portable folder", got)
	}
}

func TestRemoveDataDirsDeletesWhatExists(t *testing.T) {
	portable := filepath.Join(t.TempDir(), "AlceaMesh")
	dirs := []string{
		filepath.Join(portable, "data"),
		filepath.Join(portable, "roaming", AppName),
		filepath.Join(portable, "local", AppName),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	t.Setenv("AppData", filepath.Join(portable, "roaming"))
	t.Setenv("LocalAppData", filepath.Join(portable, "local"))

	if err := removeDataDirs(portable, func(string, ...any) {}); err != nil {
		t.Fatalf("removeDataDirs: %v", err)
	}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s must be gone, stat err = %v", dir, err)
		}
	}

	if err := removeDataDirs(portable, func(string, ...any) {}); err != nil {
		t.Errorf("a second run must not fail: %v", err)
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
	found := false
	for _, line := range lines {
		if line == "Папка установки: "+dir {
			found = true
		}
	}
	if !found {
		t.Errorf("the log must name the target folder, got %v", lines)
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
