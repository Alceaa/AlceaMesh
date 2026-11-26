package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"alceacore.ru/alceamesh/internal/datadir"
)

const fileName = "settings.json"

type Settings struct {
	DeviceID       string `json:"device_id,omitempty"`
	DeviceName     string `json:"device_name,omitempty"`
	TunnelName     string `json:"tunnel_name,omitempty"`
	ConfigText     string `json:"config_text,omitempty"`
	AcceptFiles    bool   `json:"accept_files,omitempty"`
	InboxDir       string `json:"inbox_dir,omitempty"`
	SpeedLimitKBps int    `json:"speed_limit_kbps,omitempty"`
	AutoUpdate     *bool  `json:"auto_update,omitempty"`
	UpdateManifest string `json:"update_manifest,omitempty"`
}

func Dir() string {
	return datadir.Dir()
}

func LegacyDir() string {
	return datadir.LegacyDir()
}

func Load() (*Settings, error) {
	dir := datadir.Dir()
	return loadMigrating(dir, datadir.LegacyDir(), portableTo(dir))
}

func portableTo(dir string) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Dir(filepath.Clean(dir)), filepath.Clean(filepath.Dir(exe)))
}

func (s *Settings) Save() error {
	return s.SaveTo(datadir.Dir())
}

func loadMigrating(dir, legacy string, removeLegacy bool) (*Settings, error) {
	if exists(filepath.Join(dir, fileName)) {
		return LoadFrom(dir)
	}
	if legacy == "" || filepath.Clean(legacy) == filepath.Clean(dir) || !exists(filepath.Join(legacy, fileName)) {
		return LoadFrom(dir)
	}

	migrated, err := LoadFrom(legacy)
	if err != nil {
		return LoadFrom(dir)
	}
	if err := migrated.SaveTo(dir); err != nil {
		return migrated, nil
	}
	if removeLegacy {
		_ = os.Remove(filepath.Join(legacy, fileName))
	}
	return migrated, nil
}

func LoadFrom(dir string) (*Settings, error) {
	data, err := os.ReadFile(filepath.Join(dir, fileName))
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
	return os.WriteFile(filepath.Join(dir, fileName), data, 0o600)
}

func exists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
