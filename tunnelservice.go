package main

import (
	"fmt"
	"os"
	"strings"

	"alceacore.ru/alceamesh/internal/client"
	"alceacore.ru/alceamesh/internal/ipc"
	"alceacore.ru/alceamesh/internal/naming"
	"alceacore.ru/alceamesh/internal/settings"
	"alceacore.ru/alceamesh/internal/wgconf"
)

const networkName = "Alcea Private Network"

type TunnelService struct{}

func NewTunnelService() *TunnelService {
	return &TunnelService{}
}

type ConnectionStatus struct {
	Configured bool   `json:"configured"`
	Running    bool   `json:"running"`
	Name       string `json:"name"`
	Network    string `json:"network"`
	Address    string `json:"address"`
	Endpoint   string `json:"endpoint"`
	Peers      int    `json:"peers"`
	ServiceUp  bool   `json:"serviceUp"`
	Message    string `json:"message"`
}

type ConfigSummary struct {
	Name       string   `json:"name"`
	Address    string   `json:"address"`
	Endpoint   string   `json:"endpoint"`
	PublicKey  string   `json:"publicKey"`
	AllowedIPs []string `json:"allowedIPs"`
}

func (s *TunnelService) Status() (ConnectionStatus, error) {
	stored, err := settings.Load()
	if err != nil {
		return ConnectionStatus{}, err
	}

	status := ConnectionStatus{
		Configured: strings.TrimSpace(stored.ConfigText) != "",
		Name:       displayName(stored),
		Network:    networkName,
	}
	if status.Configured {
		if config, err := wgconf.Parse([]byte(stored.ConfigText)); err == nil {
			status.Address = config.PrimaryAddress()
			status.Peers = len(config.Peers)
			if len(config.Peers) > 0 {
				status.Endpoint = config.Peers[0].Endpoint
			}
		}
	}

	conn, err := client.Dial()
	if err != nil {
		status.Message = "Служба AlceaMesh не запущена"
		return status, nil
	}
	defer conn.Close()
	status.ServiceUp = true

	result, err := conn.Status()
	if err != nil {
		status.Message = err.Error()
		return status, nil
	}
	for _, tunnel := range result.Tunnels {
		if tunnel.Name == status.Name && tunnel.Running {
			status.Running = true
		}
	}
	return status, nil
}

func (s *TunnelService) Connect() error {
	stored, err := settings.Load()
	if err != nil {
		return err
	}
	if strings.TrimSpace(stored.ConfigText) == "" {
		return fmt.Errorf("Сначала импортируйте конфигурацию WireGuard")
	}
	config, err := wgconf.Parse([]byte(stored.ConfigText))
	if err != nil {
		return fmt.Errorf("Сохранённая конфигурация повреждена: %w", err)
	}

	conn, err := dialDaemon()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.TunnelUp(ipc.TunnelUpParams{Name: displayName(stored), ConfigText: string(config.Marshal())})
	return err
}

func (s *TunnelService) Disconnect() error {
	stored, err := settings.Load()
	if err != nil {
		return err
	}
	conn, err := dialDaemon()
	if err != nil {
		return err
	}
	defer conn.Close()

	name := displayName(stored)
	result, err := conn.Status()
	if err != nil {
		return err
	}
	for _, tunnel := range result.Tunnels {
		if tunnel.Name == name && tunnel.Running {
			return conn.TunnelDown(name)
		}
	}
	return nil
}

func (s *TunnelService) Import() (ConfigSummary, error) {
	if appInstance == nil {
		return ConfigSummary{}, fmt.Errorf("Окно недоступно")
	}
	path, err := appInstance.Dialog.OpenFile().
		SetTitle("Import WireGuard configuration").
		AddFilter("WireGuard configuration", "*.conf").
		AddFilter("All files", "*").
		PromptForSingleSelection()
	if err != nil {
		return ConfigSummary{}, err
	}
	if path == "" {
		return ConfigSummary{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ConfigSummary{}, err
	}
	return saveConfig(naming.TunnelFromPath(path), data)
}

func (s *TunnelService) Forget() error {
	stored, err := settings.Load()
	if err != nil {
		return err
	}
	stored.TunnelName = ""
	stored.ConfigText = ""
	return stored.Save()
}

func (s *TunnelService) Config() (string, error) {
	stored, err := settings.Load()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(stored.ConfigText), nil
}

func saveConfig(name string, data []byte) (ConfigSummary, error) {
	config, err := wgconf.Parse(data)
	if err != nil {
		return ConfigSummary{}, fmt.Errorf("Некорректная конфигурация: %w", err)
	}

	stored, err := settings.Load()
	if err != nil {
		return ConfigSummary{}, err
	}
	stored.TunnelName = name
	stored.ConfigText = string(config.Marshal())
	if stored.DeviceName == "" {
		if host, err := os.Hostname(); err == nil {
			stored.DeviceName = host
		}
	}
	if err := stored.Save(); err != nil {
		return ConfigSummary{}, err
	}
	return summarize(name, config), nil
}

func summarize(name string, config *wgconf.Config) ConfigSummary {
	summary := ConfigSummary{Name: name, Address: config.PrimaryAddress()}
	if key, err := config.PublicKey(); err == nil {
		summary.PublicKey = key
	}
	for _, peer := range config.Peers {
		if summary.Endpoint == "" && peer.Endpoint != "" {
			summary.Endpoint = peer.Endpoint
		}
		summary.AllowedIPs = append(summary.AllowedIPs, peer.AllowedIPs...)
	}
	return summary
}

func displayName(stored *settings.Settings) string {
	if name := strings.TrimSpace(stored.TunnelName); name != "" {
		return name
	}
	return "AlceaMesh"
}

func dialDaemon() (*client.Client, error) {
	conn, err := client.Dial()
	if err != nil {
		return nil, fmt.Errorf("Служба AlceaMesh не запущена — запустите alceameshd: %w", err)
	}
	return conn, nil
}
