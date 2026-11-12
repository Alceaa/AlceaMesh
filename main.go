package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

const appVersion = "1.0.1"

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
