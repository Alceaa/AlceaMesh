package main

import (
	"errors"
	"strings"

	"alceacore.ru/alceamesh/internal/client"
)

type ServiceState struct {
	Up          bool   `json:"up"`
	Installed   bool   `json:"installed"`
	NeedsRights bool   `json:"needsRights"`
	Message     string `json:"message"`
}

func serviceReachable() bool {
	conn, err := client.Dial()
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (m *MetaService) StartService(asAdmin bool) (ServiceState, error) {
	if serviceReachable() {
		return ServiceState{Up: true, Installed: true, Message: "Служба AlceaMesh уже запущена."}, nil
	}

	if _, err := serviceStart(); err == nil {
		return ServiceState{Up: true, Installed: true, Message: "Служба AlceaMesh запущена."}, nil
	}

	if _, err := serviceQuery(); err != nil && strings.Contains(err.Error(), "1060") {
		return ServiceState{
			Installed: false,
			Message:   "Служба AlceaMesh не установлена. Запустите установщик AlceaMesh.",
		}, nil
	}

	if !asAdmin {
		return ServiceState{
			Installed:   true,
			NeedsRights: true,
			Message:     "Для запуска службы нужны права администратора.",
		}, nil
	}

	if err := runElevated("sc.exe", "start", serviceName); err != nil {
		return ServiceState{Installed: true}, friendlyError(err, fallbackService)
	}
	return ServiceState{
		Installed: true,
		Message:   "Запускаю службу с правами администратора — подтвердите запрос Windows, затем нажмите «Обновить».",
	}, nil
}

func (m *MetaService) ServiceRunning() bool {
	return serviceReachable()
}

var errServiceUnavailable = errors.New("служба AlceaMesh недоступна")
