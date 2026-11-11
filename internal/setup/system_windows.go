//go:build windows

package setup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"alceacore.ru/alceamesh/internal/autostart"
)

const swShownormal = 1

func Elevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

func Elevate(args string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	parameters := (*uint16)(nil)
	if args != "" {
		parameters, err = windows.UTF16PtrFromString(args)
		if err != nil {
			return err
		}
	}
	return windows.ShellExecute(0, verb, file, parameters, nil, swShownormal)
}

func RunPowerShell(script string) (string, error) {
	return run("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
}

func run(name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text != "" {
			return text, fmt.Errorf("%v: %s", err, text)
		}
		return text, err
	}
	return text, nil
}

func WriteUninstallRegistry(values map[string]string) error {
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, UninstallKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	for name, value := range values {
		if err := key.SetStringValue(name, value); err != nil {
			return err
		}
	}
	return nil
}

func DeleteUninstallRegistry() error {
	err := registry.DeleteKey(registry.LOCAL_MACHINE, UninstallKey)
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func SetAppAutostart(enabled bool, exePath string) error {
	return autostart.Set(enabled, exePath)
}

const appWasRunningMarker = "ALCEAMESH-APP-WAS-RUNNING"

func stopRunningComponents(report func(string, ...any)) bool {
	script := fmt.Sprintf(`$app = Get-Process alceamesh -ErrorAction SilentlyContinue
if ($app) { $app | Stop-Process -Force; Start-Sleep -Milliseconds 800; '%s' }
sc.exe stop %s | Out-Null
`, appWasRunningMarker, ServiceName)

	output, err := RunPowerShell(script)
	if err != nil {
		report("Не удалось остановить компоненты: %v", err)
	}
	return strings.Contains(output, appWasRunningMarker)
}

func startApp(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	command := exec.Command("explorer.exe", path)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return command.Start()
}

func installService(exePath string, report func(string, ...any)) error {
	if output, err := run(exePath, "install"); err != nil {
		if strings.Contains(output, "already installed") {
			report("Служба уже установлена")
		} else {
			return err
		}
	}

	output, err := run("sc.exe", "start", ServiceName)
	if err != nil && !strings.Contains(strings.ToLower(output), "already") && !strings.Contains(strings.ToLower(output), "уже") {
		return fmt.Errorf("запуск службы: %v", err)
	}
	return nil
}

func removeService(dir string, report func(string, ...any)) error {
	_, _ = run("sc.exe", "stop", ServiceName)

	exe := filepath.Join(dir, "alceameshd.exe")
	if _, err := os.Stat(exe); err == nil {
		if _, err := run(exe, "uninstall"); err != nil {
			report("Деинсталлятор службы не сработал (%v), пробую sc delete", err)
			_, _ = run("sc.exe", "delete", ServiceName)
		}
		return nil
	}
	_, _ = run("sc.exe", "delete", ServiceName)
	return nil
}
