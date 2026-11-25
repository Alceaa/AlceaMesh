package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"alceacore.ru/alceamesh/internal/setup"
	"alceacore.ru/alceamesh/internal/version"
)

//go:embed all:web
var webAssets embed.FS

//go:embed all:payload
var payloadAssets embed.FS

var appInstance *application.App

func main() {
	payload, err := fs.Sub(payloadAssets, "payload")
	if err != nil {
		payload = payloadAssets
	}

	options := setup.Options{
		Dir:       setup.PreferredDir(),
		Version:   version.String(),
		Service:   true,
		Firewall:  true,
		Shortcuts: true,
	}
	quiet := false
	mode := "install"

	for index, arg := range os.Args[1:] {
		switch {
		case strings.EqualFold(arg, "--uninstall"):
			mode = "uninstall"
		case strings.EqualFold(arg, "--quiet"):
			quiet = true
		case strings.EqualFold(arg, "--dry-run"):
			options.DryRun = true
		case strings.EqualFold(arg, "--purge"):
			options.Purge = true
		case strings.EqualFold(arg, "--dir") && index+2 < len(os.Args):
			options.Dir = os.Args[index+2]
		case strings.HasPrefix(strings.ToLower(arg), "--dir="):
			options.Dir = arg[len("--dir="):]
		}
	}

	options.PreviousDir = setup.InstalledDir()

	if quiet {
		os.Exit(runQuiet(payload, mode, options))
	}

	state := &state{payload: payload, options: options, mode: mode}

	appInstance = application.New(application.Options{
		Name:        "AlceaMesh Setup",
		Description: "AlceaMesh installer",
		Assets: application.AssetOptions{
			Handler: state,
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: webviewDataDir(options.Dir),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	appInstance.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Установка AlceaMesh",
		Width:            720,
		Height:           600,
		MinWidth:         640,
		MinHeight:        520,
		BackgroundColour: application.NewRGB(5, 5, 6),
		URL:              "/",
		Windows: application.WindowsWindow{
			Theme: application.Dark,
		},
	})

	if err := appInstance.Run(); err != nil {
		writeLog("AlceaMesh Setup: " + err.Error())
		os.Exit(1)
	}
}

func runQuiet(payload fs.FS, mode string, options setup.Options) int {
	if !options.DryRun && !setup.Elevated() {
		if err := setup.Elevate(commandLine(os.Args[1:])); err != nil {
			writeLog("нужны права администратора: " + err.Error())
			return 2
		}
		return 0
	}

	self, err := os.Executable()
	if err != nil {
		self = ""
	}

	if mode == "uninstall" {
		writeLog("Режим: удаление")
	} else {
		writeLog("Режим: установка")
		for _, line := range setup.Plan(optionsForMode(mode, options, self)) {
			writeLog("план: " + line)
		}
	}

	var runErr error
	if mode == "uninstall" {
		options = optionsForMode(mode, options, self)
		runErr = setup.Uninstall(options, self, writeLog)
	} else {
		runErr = setup.Install(payload, self, options, writeLog)
	}
	if runErr != nil {
		writeLog("ОШИБКА: " + runErr.Error())
		return 1
	}
	return 0
}

func optionsForMode(mode string, options setup.Options, self string) setup.Options {
	if mode == "uninstall" && (options.Dir == "" || options.Dir == setup.DefaultDir()) {
		if dir := setup.DirFromExecutable(self); dir != "" {
			options.Dir = dir
		}
	}
	return options
}

func commandLine(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.ContainsAny(arg, " \t") {
			quoted = append(quoted, `"`+arg+`"`)
			continue
		}
		quoted = append(quoted, arg)
	}
	return strings.Join(quoted, " ")
}

func writeLog(line string) {
	path := filepath.Join(os.TempDir(), "alceamesh-setup.log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintln(file, line)
}

func webviewDataDir(target string) string {
	candidates := make([]string, 0, 2)
	if strings.TrimSpace(target) != "" {
		candidates = append(candidates, filepath.Join(target, "data", "webview-setup"))
	}
	candidates = append(candidates, filepath.Join(os.TempDir(), "AlceaMesh-setup-webview"))

	for _, candidate := range candidates {
		if err := os.MkdirAll(candidate, 0o755); err == nil {
			return candidate
		}
	}
	return ""
}
