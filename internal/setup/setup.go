package setup

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	ServiceName  = "AlceaMesh"
	AppName      = "AlceaMesh"
	Publisher    = "Alcea"
	Description  = "AlceaMesh — передача файлов и VPN"
	UninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\AlceaMesh`
	uninstallExe = "alceamesh-setup.exe"
)

type Options struct {
	Dir         string
	PreviousDir string
	Version     string
	Autostart   bool
	Shortcuts   bool
	Service     bool
	Firewall    bool
	Purge       bool
	DryRun      bool
}

type step struct {
	title string
	run   func() error
}

func PayloadFiles() []string {
	return []string{"alceamesh.exe", "alceameshd.exe", "wintun.dll"}
}

func DefaultDir() string {
	base := strings.TrimSpace(os.Getenv("ProgramFiles"))
	if base == "" {
		base = `C:\Program Files`
	}
	return filepath.Join(base, AppName)
}

func PreferredDir() string {
	if dir := InstalledDir(); dir != "" {
		return dir
	}
	return DefaultDir()
}

func DirFromExecutable(selfPath string) string {
	if strings.TrimSpace(selfPath) == "" {
		return ""
	}
	dir := filepath.Dir(filepath.Clean(selfPath))
	if !looksLikeOurInstall(dir) {
		return ""
	}
	return dir
}

func Validate(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return errors.New("Укажите папку для установки.")
	}
	if !filepath.IsAbs(dir) {
		return errors.New("Нужен полный путь, например C:\\Program Files\\AlceaMesh")
	}
	cleaned := filepath.Clean(dir)
	if filepath.Dir(cleaned) == cleaned {
		return errors.New("Нельзя устанавливать в корень диска.")
	}
	lower := strings.ToLower(cleaned)
	for _, blocked := range []string{`\windows`, `\windows\system32`, `\program files\windows`} {
		if strings.HasSuffix(lower, blocked) {
			return errors.New("Эта папка системная — выберите другую.")
		}
	}
	return nil
}

func Plan(options Options) []string {
	steps := []string{
		"Остановить приложение и службу AlceaMesh, если они запущены",
		fmt.Sprintf("Скопировать файлы в %s", options.Dir),
	}
	if stale := removalCandidates(options.Dir, options.PreviousDir, DefaultDir()); len(stale) > 0 {
		steps = append(steps, fmt.Sprintf("Удалить прежнюю копию программы из %s", strings.Join(stale, ", ")))
	}
	steps = append(steps, "Убрать данные, оставшиеся в системных папках от прежних версий")
	if !protectedInstall(options.Dir) {
		steps = append(steps, "Разрешить папке data запись обычному пользователю (настройки лежат рядом с программой)")
	}
	steps = append(steps, "Создать папку для настроек и кеша интерфейса")
	if options.Service {
		steps = append(steps, "Установить службу AlceaMesh (нужна для VPN) и запустить её")
	}
	if options.Firewall {
		steps = append(steps, "Разрешить приложению UDP в брандмауэре Windows")
	}
	if options.Shortcuts {
		steps = append(steps, `Ярлык в меню «Пуск»`, "Ярлык на рабочем столе")
	}
	if options.Autostart {
		steps = append(steps, "Запускать приложение при входе в систему")
	} else {
		steps = append(steps, "Автозапуск не включать")
	}
	steps = append(steps, "Добавить AlceaMesh в «Установка и удаление программ»")
	return steps
}

func Install(payload fs.FS, selfPath string, options Options, log func(string)) error {
	if err := Validate(options.Dir); err != nil {
		return err
	}
	report := func(format string, args ...any) {
		if log != nil {
			log(fmt.Sprintf(format, args...))
		}
	}
	report("Папка установки: %s", options.Dir)
	appRunning := false

	knownDirs := removalCandidates(options.Dir, options.PreviousDir, DefaultDir())

	tasks := []step{
		{title: "Останавливаю запущенные компоненты", run: func() error {
			appRunning = stopRunningComponents(report)
			return nil
		}},
		{title: "Создаю папку", run: func() error {
			return os.MkdirAll(options.Dir, 0o755)
		}},
	}

	for _, name := range PayloadFiles() {
		name := name
		tasks = append(tasks, step{
			title: "Копирую " + name,
			run: func() error {
				return copyFromFS(payload, name, filepath.Join(options.Dir, name))
			},
		})
	}

	tasks = append(tasks, step{
		title: "Копирую деинсталлятор",
		run: func() error {
			return copyFile(selfPath, filepath.Join(options.Dir, uninstallExe))
		},
	})

	tasks = append(tasks,
		step{title: "Убираю прежнюю копию программы", run: func() error {
			removePreviousInstall(knownDirs, report)
			return nil
		}},
		step{title: "Убираю старые служебные данные", run: func() error {
			cleanLegacyCaches(report)
			return nil
		}},
	)

	tasks = append(tasks, step{
		title: "Готовлю папку для настроек",
		run: func() error {
			dataDir := filepath.Join(options.Dir, "data")
			if err := os.MkdirAll(dataDir, 0o755); err != nil {
				return err
			}
			stale := filepath.Join(dataDir, "webview-setup")
			if dirExists(stale) {
				if err := os.RemoveAll(stale); err != nil {
					report("Не удалось убрать старый кеш установщика: %v", err)
				} else {
					report("Убрал старый кеш установщика из папки установки")
				}
			}
			if protectedInstall(options.Dir) {
				report("Папка установки системная: приложение будет хранить настройки в профиле пользователя")
				return nil
			}
			_, err := RunPowerShell(DataDirScript(dataDir))
			return err
		},
	})

	if options.Service {
		tasks = append(tasks, step{
			title: "Устанавливаю службу",
			run: func() error {
				return installService(filepath.Join(options.Dir, "alceameshd.exe"), report)
			},
		})
	}

	if options.Firewall {
		tasks = append(tasks, step{
			title: "Настраиваю брандмауэр",
			run: func() error {
				for _, name := range []string{"alceamesh.exe", "alceameshd.exe"} {
					if _, err := RunPowerShell(FirewallScript(filepath.Join(options.Dir, name))); err != nil {
						return err
					}
				}
				return nil
			},
		})
	}

	if options.Shortcuts {
		tasks = append(tasks, step{
			title: "Создаю ярлыки",
			run: func() error {
				exe := filepath.Join(options.Dir, "alceamesh.exe")
				_, err := RunPowerShell(ShortcutScript(exe, options.Dir, Description))
				return err
			},
		})
	}

	tasks = append(tasks,
		step{title: "Записываю запись об установке", run: func() error {
			return WriteUninstallRegistry(UninstallRegistryValues(options.Dir, options.Version))
		}},
		step{title: "Настраиваю автозапуск", run: func() error {
			return SetAppAutostart(options.Autostart, filepath.Join(options.Dir, "alceamesh.exe"))
		}},
	)

	for _, task := range tasks {
		report("%s…", task.title)
		if options.DryRun {
			continue
		}
		if err := task.run(); err != nil {
			return fmt.Errorf("%s: %w", strings.ToLower(task.title), err)
		}
	}

	if appRunning && !options.DryRun {
		report("Запускаю приложение заново…")
		if err := startApp(filepath.Join(options.Dir, "alceamesh.exe")); err != nil {
			report("Не удалось запустить приложение: %v", err)
		}
	}
	report("Готово.")
	return nil
}

func Uninstall(options Options, selfPath string, log func(string)) error {
	report := func(format string, args ...any) {
		if log != nil {
			log(fmt.Sprintf(format, args...))
		}
	}
	dir := filepath.Clean(options.Dir)
	report("Папка установки: %s", dir)

	tasks := []step{
		{title: "Останавливаю службу", run: func() error {
			return removeService(dir, report)
		}},
		{title: "Убираю правила брандмауэра", run: func() error {
			_, err := RunPowerShell(RemoveFirewallScript())
			return err
		}},
		{title: "Удаляю ярлыки", run: func() error {
			_, err := RunPowerShell(RemoveShortcutsScript())
			return err
		}},
		{title: "Выключаю автозапуск", run: func() error {
			return SetAppAutostart(false, "")
		}},
		{title: "Убираю запись об установке", run: func() error {
			return DeleteUninstallRegistry()
		}},
		{title: "Удаляю файлы программы", run: func() error {
			return removeProgramFiles(dir, selfPath, report)
		}},
	}

	if options.Purge {
		tasks = append(tasks, step{title: "Удаляю настройки", run: func() error {
			return removeDataDirs(dir, report)
		}})
	}

	for _, task := range tasks {
		report("%s…", task.title)
		if options.DryRun {
			continue
		}
		if err := task.run(); err != nil {
			return fmt.Errorf("%s: %w", strings.ToLower(task.title), err)
		}
	}
	report("Готово.")
	return nil
}

func previousInstall(previous, current string) string {
	stale := strings.TrimSpace(previous)
	if stale == "" {
		return ""
	}
	stale = filepath.Clean(stale)
	target := filepath.Clean(strings.TrimSpace(current))
	if strings.EqualFold(stale, target) {
		return ""
	}
	if within(target, stale) {
		return ""
	}
	if !looksLikeOurInstall(stale) {
		return ""
	}
	return stale
}

func removalCandidates(current string, previous ...string) []string {
	var stale []string
	for _, candidate := range previous {
		if dir := previousInstall(candidate, current); dir != "" {
			stale = append(stale, dir)
		}
	}
	return stale
}

func within(child, parent string) bool {
	child = strings.ToLower(filepath.Clean(child))
	parent = strings.ToLower(filepath.Clean(parent))
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+string(os.PathSeparator))
}

func looksLikeOurInstall(dir string) bool {
	for _, name := range PayloadFiles() {
		if name == "wintun.dll" {
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func protectedInstall(dir string) bool {
	cleaned := strings.ToLower(filepath.Clean(strings.TrimSpace(dir)))
	if cleaned == "" || cleaned == "." {
		return false
	}
	roots := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramW6432"), `C:\Program Files`}
	for _, root := range roots {
		root = strings.ToLower(strings.TrimSpace(root))
		if root == "" {
			continue
		}
		if cleaned == root || strings.HasPrefix(cleaned, root+string(os.PathSeparator)) {
			return true
		}
	}
	windows := strings.ToLower(strings.TrimSpace(os.Getenv("SystemRoot")))
	return windows != "" && strings.HasPrefix(cleaned, windows+string(os.PathSeparator))
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func removePreviousInstall(stale []string, report func(string, ...any)) {
	for _, dir := range stale {
		report("Удаляю прежнюю копию программы из %s", dir)
		if err := os.RemoveAll(dir); err != nil {
			report("Не удалось удалить %s: %v", dir, err)
		}
	}
}

func dataDirs(dir string) []string {
	dirs := []string{filepath.Join(filepath.Clean(dir), "data")}
	if base := strings.TrimSpace(os.Getenv("AppData")); base != "" {
		dirs = append(dirs, filepath.Join(base, AppName))
	}
	if base := strings.TrimSpace(os.Getenv("LocalAppData")); base != "" {
		dirs = append(dirs, filepath.Join(base, AppName))
	}
	return dirs
}

func removeDataDirs(dir string, report func(string, ...any)) error {
	var firstErr error
	for _, candidate := range dataDirs(dir) {
		info, err := os.Stat(candidate)
		if err != nil || !info.IsDir() {
			continue
		}
		report("Удаляю %s", candidate)
		if err := os.RemoveAll(candidate); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func copyFromFS(source fs.FS, name, target string) error {
	file, err := source.Open(name)
	if err != nil {
		return fmt.Errorf("нет файла %s в установщике: %w", name, err)
	}
	defer file.Close()
	return writeFile(target, file)
}

func copyFile(source, target string) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	return writeFile(target, file)
}

func writeFile(target string, source io.Reader) error {
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, source); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func removeProgramFiles(dir, selfPath string, report func(string, ...any)) error {
	if !strings.EqualFold(filepath.Dir(filepath.Clean(selfPath)), dir) {
		return os.RemoveAll(dir)
	}
	report("Деинсталлятор запущен из папки установки, удаляю содержимое по частям")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if strings.EqualFold(path, selfPath) {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}
