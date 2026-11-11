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
	Dir       string
	Version   string
	Autostart bool
	Shortcuts bool
	Service   bool
	Firewall  bool
	Purge     bool
	DryRun    bool
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
	appRunning := false

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
			return os.RemoveAll(settingsDir())
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
