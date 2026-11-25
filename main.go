package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"alceacore.ru/alceamesh/internal/datadir"
)

//go:embed all:frontend/dist
var assets embed.FS

var (
	appInstance    *application.App
	startMinimised bool
)

func main() {
	peerService := NewPeerService()
	startMinimised = trayRequested()

	appInstance = application.New(application.Options{
		Name:        "AlceaMesh",
		Description: "P2P file transfer and WireGuard tunnels",
		Services: []application.Service{
			application.NewService(NewTunnelService()),
			application.NewService(NewDeviceService()),
			application.NewService(peerService),
			application.NewService(&MetaService{}),
		},
		OnShutdown: func() {
			peerService.Shutdown()
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "ru.alceacore.alceamesh",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				showMainWindow()
			},
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: webviewDataDir(),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	mainWindow = newMainWindow()
	setupTray()

	if err := appInstance.Run(); err != nil {
		log.Fatal(err)
	}
}

func webviewDataDir() string {
	dir := filepath.Join(datadir.Dir(), "webview")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	return dir
}
