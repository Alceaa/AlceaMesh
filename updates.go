package main

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"alceacore.ru/alceamesh/internal/datadir"
	"alceacore.ru/alceamesh/internal/settings"
	"alceacore.ru/alceamesh/internal/update"
	"alceacore.ru/alceamesh/internal/version"
)

const defaultUpdateManifest = "https://alceacore.ru/alceamesh/latest.json"

type UpdateState struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Notes     string `json:"notes"`
	Available bool   `json:"available"`
	Auto      bool   `json:"auto"`
	Message   string `json:"message"`
}

func updateManifestURL() string {
	if value := strings.TrimSpace(os.Getenv("ALCEAMESH_UPDATE_MANIFEST")); value != "" {
		return value
	}
	if stored, err := settings.Load(); err == nil {
		if value := strings.TrimSpace(stored.UpdateManifest); value != "" {
			return value
		}
	}
	return defaultUpdateManifest
}

func autoUpdateEnabled() bool {
	stored, err := settings.Load()
	if err != nil || stored.AutoUpdate == nil {
		return false
	}
	return *stored.AutoUpdate
}

func updateDir() string {
	return filepath.Join(datadir.Dir(), "updates")
}

func appDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

func (m *MetaService) UpdateCheck() UpdateState {
	state := UpdateState{Current: version.String(), Auto: autoUpdateEnabled()}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	release, err := update.Fetch(ctx, &http.Client{Timeout: 25 * time.Second}, updateManifestURL())
	if err != nil {
		state.Message = friendlyText(err, fallbackNetwork)
		return state
	}

	cleanupDownloads(release.Version)
	state.Latest = release.Version
	state.Notes = release.Notes
	state.Available = update.IsNewer(version.String(), release.Version)
	if !state.Available {
		state.Message = "Установлена последняя версия."
	}
	return state
}

func (m *MetaService) InstallUpdate() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	client := &http.Client{Timeout: 15 * time.Minute}
	release, err := update.Fetch(ctx, client, updateManifestURL())
	if err != nil {
		return "", friendlyError(err, fallbackNetwork)
	}
	if !update.IsNewer(version.String(), release.Version) {
		return "Обновление не требуется.", nil
	}

	installer, err := update.Download(ctx, client, release, updateDir())
	if err != nil {
		return "", friendlyError(err, fallbackTransfer)
	}
	if err := launchInstaller(installer); err != nil {
		return "", friendlyError(err, fallbackService)
	}
	quitSoon()
	return "Обновляюсь до версии " + release.Version + ". Приложение закроется и откроется заново.", nil
}

func (m *MetaService) AutoUpdate() bool {
	return autoUpdateEnabled()
}

func (m *MetaService) SetAutoUpdate(enabled bool) error {
	stored, err := settings.Load()
	if err != nil {
		return friendlyError(err, fallbackSettings)
	}
	stored.AutoUpdate = &enabled
	return friendlyError(stored.Save(), fallbackSettings)
}

func (m *MetaService) UpdateSource() string {
	return updateManifestURL()
}

func launchInstaller(path string) error {
	args := []string{"--quiet"}
	if dir := appDir(); dir != "" {
		args = append(args, "--dir", dir)
	}
	command := exec.Command(path, args...)
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func quitSoon() {
	go func() {
		time.Sleep(1500 * time.Millisecond)
		if appInstance != nil {
			appInstance.Quit()
		}
	}()
}

func cleanupDownloads(keep string) {
	dir := updateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	keepName := "AlceaMesh-Setup-" + keep + ".exe"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		lower := strings.ToLower(name)
		stale := strings.HasSuffix(lower, ".part") ||
			(strings.HasPrefix(name, "AlceaMesh-Setup-") && strings.HasSuffix(lower, ".exe") && !strings.EqualFold(name, keepName))
		if !stale {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}
