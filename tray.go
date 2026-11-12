package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed assets/icon.png
var trayIcon []byte

var (
	trayReady  bool
	mainWindow *application.WebviewWindow
)

func newMainWindow() *application.WebviewWindow {
	state := application.WindowStateNormal
	if startMinimised {
		state = application.WindowStateMinimised
	}
	return appInstance.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "AlceaMesh",
		Width:            520,
		Height:           820,
		MinWidth:         440,
		MinHeight:        620,
		StartState:       state,
		BackgroundColour: application.NewRGB(5, 5, 6),
		URL:              "/",
		Windows: application.WindowsWindow{
			Theme: application.Dark,
		},
	})
}

func setupTray() {
	tray := appInstance.SystemTray.New()
	tray.SetTooltip("AlceaMesh")
	if len(trayIcon) > 0 {
		tray.SetIcon(trayIcon)
	}

	menu := application.NewMenu()
	menu.Add("Открыть AlceaMesh").OnClick(func(*application.Context) {
		showMainWindow()
	})
	menu.Add("Свернуть в трей").OnClick(func(*application.Context) {
		hideToTray()
	})
	menu.AddSeparator()
	menu.Add("Выход").OnClick(func(*application.Context) {
		appInstance.Quit()
	})
	tray.SetMenu(menu)

	tray.OnClick(func() {
		showMainWindow()
	})

	trayReady = true
}

func showMainWindow() {
	if appInstance == nil {
		return
	}
	if mainWindow == nil {
		mainWindow = newMainWindow()
	}
	mainWindow.Restore()
	mainWindow.Show()
	mainWindow.Focus()
}

func hideToTray() {
	if appInstance == nil {
		return
	}
	appInstance.Hide()
}
