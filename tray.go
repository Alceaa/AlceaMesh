package main

import (
	_ "embed"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed assets/icon.png
var trayIcon []byte

var trayReady bool

func setupTray(window *application.WebviewWindow) {
	tray := appInstance.SystemTray.New()
	tray.SetTooltip("AlceaMesh")
	if len(trayIcon) > 0 {
		tray.SetIcon(trayIcon)
	}

	menu := application.NewMenu()
	menu.Add("Открыть AlceaMesh").OnClick(func(*application.Context) {
		showMainWindow(window)
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
		showMainWindow(window)
	})

	trayReady = true
}

func showMainWindow(window *application.WebviewWindow) {
	if window == nil {
		return
	}
	window.Show()
	window.Focus()
}

func hideToTray() {
	if appInstance == nil {
		return
	}
	appInstance.Hide()
}
