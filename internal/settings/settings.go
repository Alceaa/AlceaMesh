package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Settings struct {
	DeviceName     string `json:"device_name,omitempty"`
	TunnelName     string `json:"tunnel_name,omitempty"`
	ConfigText     string `json:"config_text,omitempty"`
	AcceptFiles    bool   `json:"accept_files,omitempty"`
	InboxDir       string `json:"inbox_dir,omitempty"`
	SpeedLimitKBps int    `json:"speed_limit_kbps,omitempty"`
}

func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "AlceaMesh"), nil
}

func Load() (*Settings, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return LoadFrom(dir)
}

func (s *Settings) Save() error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	return s.SaveTo(dir)
}

func LoadFrom(dir string) (*Settings, error) {
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if os.IsNotExist(err) {
		return &Settings{}, nil
	}
	if err != nil {
		return nil, err
	}
	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

func (s *Settings) SaveTo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o600)
}
