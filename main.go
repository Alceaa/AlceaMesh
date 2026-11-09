package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

const appVersion = "0.3.0-m3"

var appInstance *application.App

func main() {
	appInstance = application.New(application.Options{
		Name:        "AlceaMesh",
		Description: "P2P file transfer and WireGuard tunnels",
		Services: []application.Service{
			application.NewService(NewTunnelService()),
			application.NewService(NewDeviceService()),
			application.NewService(&MetaService{}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	appInstance.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "AlceaMesh",
		Width:            520,
		Height:           820,
		MinWidth:         440,
		MinHeight:        620,
		BackgroundColour: application.NewRGB(5, 5, 6),
		URL:              "/",
		Windows: application.WindowsWindow{
			Theme: application.Dark,
		},
	})

	if err := appInstance.Run(); err != nil {
		log.Fatal(err)
	}
}
