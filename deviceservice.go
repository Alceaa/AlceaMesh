package main

import (
	"errors"
	"os"
	"runtime"
	"sort"
	"strings"

	"alceacore.ru/alceamesh/internal/coordclient"
	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/identity"
	"alceacore.ru/alceamesh/internal/settings"
	"alceacore.ru/alceamesh/internal/wgconf"
)

const coordinatorBaseURL = "https://mesh.alceacore.ru"

type DeviceService struct{}

func NewDeviceService() *DeviceService {
	return &DeviceService{}
}

type Identity struct {
	Name      string `json:"name"`
	MeshIP    string `json:"meshIP"`
	PublicKey string `json:"publicKey"`
	Platform  string `json:"platform"`
	Accept    bool   `json:"accept"`
}

type DeviceView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MeshIP    string `json:"meshIP"`
	Platform  string `json:"platform"`
	PublicKey string `json:"publicKey"`
	Online    bool   `json:"online"`
	Available bool   `json:"available"`
	Self      bool   `json:"self"`
}

type NetworkView struct {
	Coordinator string       `json:"coordinator"`
	Reachable   bool         `json:"reachable"`
	Message     string       `json:"message"`
	DeviceID    string       `json:"deviceID"`
	Signing     string       `json:"signing"`
	Identity    Identity     `json:"identity"`
	Devices     []DeviceView `json:"devices"`
}

const (
	signingUnregistered = "unregistered"
	signingPending      = "pending"
	signingAttached     = "attached"
	signingRevoked      = "revoked"
)

func (s *DeviceService) Network() (NetworkView, error) {
	stored, err := settings.Load()
	if err != nil {
		return NetworkView{}, err
	}

	view := NetworkView{Coordinator: coordinatorURL(), Identity: identityOf(stored), Signing: signingUnregistered}
	if view.Identity.PublicKey == "" {
		view.Message = "Импортируйте конфигурацию, чтобы войти в сеть."
		return view, nil
	}
	if !vpnRunning(stored) {
		view.Message = "Включите VPN, чтобы увидеть устройства сети."
		view.Signing = signingState(stored, nil)
		return view, nil
	}

	client, err := newCoordinatorClient(deviceCredentials(stored))
	if err != nil {
		view.Message = friendlyText(err, fallbackNetwork)
		return view, nil
	}

	self, err := client.Announce(coordclient.PresenceRequest{
		PublicKey: view.Identity.PublicKey,
		Name:      view.Identity.Name,
		MeshIP:    view.Identity.MeshIP,
		Platform:  view.Identity.Platform,
		Available: stored.AcceptFiles,
	})
	if err != nil {
		view.Message = friendlyText(err, fallbackNetwork)
		return view, nil
	}
	view.Reachable = true
	view.Coordinator = client.ActiveEndpoint().BaseURL

	self, stored = s.syncIdentity(client, stored, self)
	view.DeviceID = self.ID
	view.Signing = signingState(stored, self)
	if self.Revoked() {
		view.Message = "Это устройство отозвано владельцем сети. Обратитесь к нему за новой регистрацией."
		view.Devices = nil
		return view, nil
	}

	peers, err := client.Peers(false)
	if err != nil {
		view.Message = friendlyText(err, fallbackNetwork)
		return view, nil
	}
	for _, peer := range peers {
		isSelf := peer.ID == self.ID
		if !visibleDevice(peer, isSelf) {
			continue
		}
		view.Devices = append(view.Devices, deviceView(peer, isSelf))
	}
	sortDevices(view.Devices)
	rememberDevices(view.Devices)
	return view, nil
}

func (s *DeviceService) syncIdentity(client *coordclient.Client, stored *settings.Settings, self *coordinator.Device) (*coordinator.Device, *settings.Settings) {
	if self == nil {
		return self, stored
	}
	changed := false

	if stored.DeviceID != self.ID {
		stored.DeviceID = self.ID
		changed = true
	}
	if changed {
		_ = stored.Save()
	}

	if self.SignPublicKey != "" || self.Revoked() {
		if self.SignPublicKey != "" && stored.SignPublicKey != self.SignPublicKey {
			stored.SignPublicKey = self.SignPublicKey
			_ = stored.Save()
		}
		return self, stored
	}

	key, err := deviceKey()
	if err != nil {
		return self, stored
	}
	attached, err := attachDevice(client, stored, key)
	if err != nil {
		return self, stored
	}
	if attached.SignPublicKey != stored.SignPublicKey {
		stored.SignPublicKey = attached.SignPublicKey
		_ = stored.Save()
	}
	return attached, stored
}

func signingState(stored *settings.Settings, device *coordinator.Device) string {
	if device != nil {
		switch {
		case device.Revoked():
			return signingRevoked
		case strings.TrimSpace(device.SignPublicKey) != "":
			return signingAttached
		default:
			return signingPending
		}
	}
	if stored == nil || strings.TrimSpace(stored.DeviceID) == "" {
		return signingUnregistered
	}
	if strings.TrimSpace(stored.SignPublicKey) != "" {
		return signingAttached
	}
	return signingPending
}

func visibleDevice(peer coordinator.DeviceView, self bool) bool {
	if self {
		return true
	}
	return peer.Available && peer.Online
}

func vpnRunning(stored *settings.Settings) bool {
	if stored == nil || strings.TrimSpace(stored.ConfigText) == "" {
		return false
	}
	return tunnelRunning(displayName(stored))
}

func (s *DeviceService) SetAccept(accept bool) error {
	stored, err := settings.Load()
	if err != nil {
		return friendlyError(err, fallbackSettings)
	}
	stored.AcceptFiles = accept
	return friendlyError(stored.Save(), fallbackSettings)
}

func (s *DeviceService) SetName(name string) error {
	stored, err := settings.Load()
	if err != nil {
		return friendlyError(err, fallbackSettings)
	}
	stored.DeviceName = strings.TrimSpace(name)
	return friendlyError(stored.Save(), fallbackSettings)
}

func (s *DeviceService) JoinWithInvite(code string) (string, error) {
	invite := strings.TrimSpace(code)
	if invite == "" {
		return "", errors.New("введите код приглашения")
	}

	stored, err := settings.Load()
	if err != nil {
		return "", friendlyError(err, fallbackSettings)
	}
	key, err := deviceKey()
	if err != nil {
		return "", friendlyError(err, fallbackSettings)
	}
	client, err := newCoordinatorClient(nil)
	if err != nil {
		return "", friendlyError(err, fallbackNetwork)
	}

	result, err := joinWithInvite(client, stored, key, invite)
	if err != nil {
		return "", friendlyError(err, fallbackNetwork)
	}

	stored.DeviceID = result.Device.ID
	stored.DeviceSecret = result.Secret
	stored.SignPublicKey = key.PublicKey()
	if err := stored.Save(); err != nil {
		return "", friendlyError(err, fallbackSettings)
	}
	return "Устройство подключено к сети: " + result.Device.Name + " · " + result.Device.MeshIP, nil
}

func joinWithInvite(client *coordclient.Client, stored *settings.Settings, key *identity.Key, code string) (*coordclient.EnrollResult, error) {
	_, _, publicKey, err := wireGuardKeys(stored)
	if err != nil {
		return nil, errors.New("сначала импортируйте конфигурацию WireGuard — без неё подключаться нечем")
	}
	return client.EnrollWithInvite(code, identityOf(stored).Name, runtime.GOOS, publicKey, key.PublicKey(), key)
}

func identityOf(stored *settings.Settings) Identity {
	identity := Identity{
		Name:     strings.TrimSpace(stored.DeviceName),
		Platform: runtime.GOOS,
		Accept:   stored.AcceptFiles,
	}
	if identity.Name == "" {
		if host, err := os.Hostname(); err == nil {
			identity.Name = host
		}
	}
	if stored.ConfigText != "" {
		if config, err := wgconf.Parse([]byte(stored.ConfigText)); err == nil {
			identity.MeshIP = config.PrimaryAddress()
			if key, err := config.PublicKey(); err == nil {
				identity.PublicKey = key
			}
		}
	}
	return identity
}

func coordinatorURL() string {
	if value := strings.TrimSpace(os.Getenv("ALCEAMESH_COORDINATOR")); value != "" {
		return value
	}
	return coordinatorBaseURL
}

func deviceView(device coordinator.DeviceView, self bool) DeviceView {
	return DeviceView{
		ID:        device.ID,
		Name:      device.Name,
		MeshIP:    device.MeshIP,
		Platform:  device.Platform,
		PublicKey: device.PublicKey,
		Online:    device.Online,
		Available: device.Available,
		Self:      self,
	}
}

func sortDevices(devices []DeviceView) {
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Self != devices[j].Self {
			return devices[i].Self
		}
		if devices[i].Online != devices[j].Online {
			return devices[i].Online
		}
		return devices[i].Name < devices[j].Name
	})
}
