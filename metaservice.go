package main

import "errors"

type MetaService struct{}

func (m *MetaService) Version() string {
	return appVersion
}

func (m *MetaService) Coordinator() string {
	return coordinatorURL()
}

func (m *MetaService) Autostart() bool {
	return autostartEnabled()
}

func (m *MetaService) SetAutostart(enabled bool) error {
	return friendlyError(setAutostart(enabled), fallbackSettings)
}

func (m *MetaService) Tray() bool {
	return trayReady
}

func (m *MetaService) HideToTray() error {
	if appInstance == nil || !trayReady {
		return errors.New("Трей недоступен.")
	}
	hideToTray()
	return nil
}
