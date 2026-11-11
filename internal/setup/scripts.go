package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"alceacore.ru/alceamesh/internal/settings"
)

func quote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

func FirewallScript(program string) string {
	base := filepath.Base(program)
	inbound := fmt.Sprintf("%s (%s, Inbound)", AppName, base)
	outbound := fmt.Sprintf("%s (%s, Outbound)", AppName, base)
	return fmt.Sprintf(`$program = '%s'
foreach ($rule in @(@{ Name = '%s'; Direction = 'Inbound' }, @{ Name = '%s'; Direction = 'Outbound' })) {
    Get-NetFirewallRule -DisplayName $rule.Name -ErrorAction SilentlyContinue | Remove-NetFirewallRule
    New-NetFirewallRule -DisplayName $rule.Name -Direction $rule.Direction -Action Allow -Program $program -Protocol UDP | Out-Null
}
`, quote(program), quote(inbound), quote(outbound))
}

func RemoveFirewallScript() string {
	return fmt.Sprintf(`Get-NetFirewallRule -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName -like '%s (*' } | Remove-NetFirewallRule
`, AppName)
}

func ShortcutScript(exe, workDir, description string) string {
	return fmt.Sprintf(`$shell = New-Object -ComObject WScript.Shell
$exe = '%s'
$targets = @(
    (Join-Path ([Environment]::GetFolderPath('CommonPrograms')) 'AlceaMesh.lnk'),
    (Join-Path ([Environment]::GetFolderPath('CommonDesktopDirectory')) 'AlceaMesh.lnk')
)
foreach ($target in $targets) {
    $shortcut = $shell.CreateShortcut($target)
    $shortcut.TargetPath = $exe
    $shortcut.WorkingDirectory = '%s'
    $shortcut.Description = '%s'
    $shortcut.IconLocation = $exe
    $shortcut.Save()
}
`, quote(exe), quote(workDir), quote(description))
}

func RemoveShortcutsScript() string {
	return `$folders = @(
    [Environment]::GetFolderPath('CommonPrograms'),
    [Environment]::GetFolderPath('CommonDesktopDirectory'),
    [Environment]::GetFolderPath('Programs'),
    [Environment]::GetFolderPath('Desktop')
)
foreach ($folder in $folders) {
    $target = Join-Path $folder 'AlceaMesh.lnk'
    if (Test-Path $target) { Remove-Item -LiteralPath $target -Force }
}
`
}

func UninstallRegistryValues(dir, version string) map[string]string {
	exe := filepath.Join(dir, "alceamesh.exe")
	uninstall := `"` + filepath.Join(dir, uninstallExe) + `"`
	return map[string]string{
		"DisplayName":          AppName,
		"DisplayVersion":       version,
		"Publisher":            Publisher,
		"InstallLocation":      dir,
		"DisplayIcon":          exe,
		"UninstallString":      uninstall + " --uninstall",
		"QuietUninstallString": uninstall + " --uninstall --quiet",
		"NoModify":             "1",
		"NoRepair":             "1",
	}
}

func settingsDir() string {
	dir, err := settings.Dir()
	if err != nil {
		return filepath.Join(os.Getenv("AppData"), AppName)
	}
	return dir
}
