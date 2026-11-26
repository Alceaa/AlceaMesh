package main

import (
	"encoding/base64"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"alceacore.ru/alceamesh/internal/coordclient"
	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/coordinator/api"
	"alceacore.ru/alceamesh/internal/identity"
	"alceacore.ru/alceamesh/internal/settings"
)

func newTestRegistry(t *testing.T) *coordinator.Registry {
	t.Helper()

	registry, err := coordinator.NewRegistry(coordinator.NewMemoryStore(), coordinator.Options{
		Pool:        "10.66.66.0/24",
		PresenceTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

func confText(t *testing.T, privateKey string) string {
	t.Helper()
	return "[Interface]\nPrivateKey = " + privateKey + "\nAddress = 10.66.66.5/24\n\n[Peer]\nPublicKey = " +
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\nEndpoint = 89.19.217.209:52828\nAllowedIPs = 0.0.0.0/0\n"
}

func TestAttachExistingDeviceAndSignRequests(t *testing.T) {
	registry := newTestRegistry(t)

	private, err := identity.NewEphemeralKey()
	if err != nil {
		t.Fatalf("NewEphemeralKey: %v", err)
	}
	wgPublic := identity.X25519PublicKey(private)

	device, err := registry.Announce(coordinator.AnnounceRequest{PublicKey: wgPublic, Name: "старый ноут", Available: true})
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if !device.Legacy || !device.Attachable() {
		t.Fatalf("the device must start as attachable: %+v", device)
	}

	stored := &settings.Settings{
		DeviceID:   device.ID,
		ConfigText: confText(t, base64.StdEncoding.EncodeToString(private.Bytes())),
	}
	key, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}

	legacy := httptest.NewServer((&api.Server{Registry: registry, AdminKey: "test", Version: "test"}).Handler())
	defer legacy.Close()

	client, err := coordclient.NewClient(coordclient.Options{
		Endpoints: []coordclient.Endpoint{{Name: "test", BaseURL: legacy.URL}},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	attached, err := attachDevice(client, stored, key)
	if err != nil {
		t.Fatalf("attachDevice: %v", err)
	}
	if attached.SignPublicKey != key.PublicKey() {
		t.Errorf("sign key = %q, want %q", attached.SignPublicKey, key.PublicKey())
	}
	if !attached.Legacy {
		t.Error("Legacy — исторический признак записи (заведена без приглашения), при привязке он не снимается")
	}
	if attached.Attachable() {
		t.Error("attaching must be one-time")
	}
	if !attached.RevokedAt.IsZero() {
		t.Error("attaching must not revive or revoke anything")
	}

	strict := httptest.NewServer((&api.Server{
		Registry:         registry,
		AdminKey:         "test",
		Version:          "test",
		RequireSignature: true,
	}).Handler())
	defer strict.Close()

	signed, err := coordclient.NewClient(coordclient.Options{
		Endpoints: []coordclient.Endpoint{{Name: "test", BaseURL: strict.URL}},
		Signer:    identity.Credentials{DeviceID: device.ID, Key: key},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	peers, err := signed.Peers(false)
	if err != nil {
		t.Fatalf("a signed request must be accepted: %v", err)
	}
	if len(peers) != 1 {
		t.Errorf("peers = %+v, want the device itself", peers)
	}

	unsigned, err := coordclient.NewClient(coordclient.Options{
		Endpoints: []coordclient.Endpoint{{Name: "test", BaseURL: strict.URL}},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	recected, err := unsigned.Peers(false)
	_ = recected
	var rejection *identity.RejectedError
	if !errors.As(err, &rejection) {
		t.Fatalf("an unsigned request must be rejected: %v", err)
	}
	if !rejection.Matches(identity.ReasonSignatureRequired) {
		t.Errorf("reason = %q, want signature_required", rejection.Reason)
	}
}

func TestAttachNeedsAConfig(t *testing.T) {
	client, err := coordclient.NewClient(coordclient.Options{
		Endpoints: []coordclient.Endpoint{{Name: "test", BaseURL: "http://127.0.0.1:1"}},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	key, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}
	if _, err := attachDevice(client, &settings.Settings{}, key); err == nil {
		t.Error("attaching without a config must fail")
	}
}

func TestWireGuardKeysFromConfig(t *testing.T) {
	private, err := identity.NewEphemeralKey()
	if err != nil {
		t.Fatalf("NewEphemeralKey: %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(private.Bytes())
	stored := &settings.Settings{ConfigText: confText(t, encoded)}

	_, parsed, public, err := wireGuardKeys(stored)
	if err != nil {
		t.Fatalf("wireGuardKeys: %v", err)
	}
	if identity.X25519PublicKey(private) != public {
		t.Errorf("public key = %q, want %q", public, identity.X25519PublicKey(private))
	}
	if parsed.PublicKey().Bytes() == nil {
		t.Error("the parsed key must be usable")
	}

	if _, _, _, err := wireGuardKeys(&settings.Settings{}); err == nil {
		t.Error("an empty config must be rejected")
	}
}

func TestCoordinatorEndpointsPreferTheMesh(t *testing.T) {
	t.Setenv("ALCEAMESH_COORDINATOR", "")
	endpoints := coordinatorEndpoints()
	if len(endpoints) != 2 {
		t.Fatalf("endpoints = %+v, want the mesh one first and the public one second", endpoints)
	}
	if endpoints[0].Name != "mesh" || endpoints[1].Name != "public" {
		t.Errorf("order = %q, %q", endpoints[0].Name, endpoints[1].Name)
	}
	for _, endpoint := range endpoints {
		if endpoint.ServerName == "" {
			t.Errorf("endpoint %q must pin the server name", endpoint.Name)
		}
	}

	t.Setenv("ALCEAMESH_COORDINATOR", "http://10.66.66.1:7788")
	custom := coordinatorEndpoints()
	if len(custom) != 1 || custom[0].BaseURL != "http://10.66.66.1:7788" {
		t.Errorf("custom endpoints = %+v", custom)
	}
}

func TestJoinWithInvite(t *testing.T) {
	registry := newTestRegistry(t)
	_, code, err := registry.CreateInvite("тест", "", 1, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}

	server := httptest.NewServer((&api.Server{Registry: registry, AdminKey: "test", Version: "test"}).Handler())
	defer server.Close()

	client, err := coordclient.NewClient(coordclient.Options{
		Endpoints: []coordclient.Endpoint{{Name: "test", BaseURL: server.URL}},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	private, err := identity.NewEphemeralKey()
	if err != nil {
		t.Fatalf("NewEphemeralKey: %v", err)
	}
	stored := &settings.Settings{
		DeviceName: "Новый ноут",
		ConfigText: confText(t, base64.StdEncoding.EncodeToString(private.Bytes())),
	}
	key, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}

	result, err := joinWithInvite(client, stored, key, code)
	if err != nil {
		t.Fatalf("joinWithInvite: %v", err)
	}
	if result.Device.ID == "" || result.Secret == "" {
		t.Fatalf("enrollment result = %+v", result)
	}
	if result.Device.SignPublicKey != key.PublicKey() {
		t.Errorf("sign key = %q, want %q", result.Device.SignPublicKey, key.PublicKey())
	}
	if result.Device.Name != "Новый ноут" {
		t.Errorf("name = %q", result.Device.Name)
	}

	device, err := registry.DeviceByID(result.Device.ID)
	if err != nil {
		t.Fatalf("DeviceByID: %v", err)
	}
	if device.SignPublicKey == "" || device.Legacy {
		t.Errorf("the registered device must carry a signing key and not be legacy: %+v", device)
	}

	if _, err := joinWithInvite(client, stored, key, code); err == nil {
		t.Error("reusing the invitation must fail")
	}
	if _, err := joinWithInvite(client, &settings.Settings{}, key, code); err == nil {
		t.Error("joining without a config must fail")
	}
}

func TestSigningState(t *testing.T) {
	cases := []struct {
		name   string
		stored *settings.Settings
		device *coordinator.Device
		want   string
	}{
		{"без настроек", nil, nil, signingUnregistered},
		{"нет устройства", &settings.Settings{}, nil, signingUnregistered},
		{"ожидает привязки", &settings.Settings{DeviceID: "d1"}, nil, signingPending},
		{"привязано по настройкам", &settings.Settings{DeviceID: "d1", SignPublicKey: "key"}, nil, signingAttached},
		{"координатор подтверждает", &settings.Settings{DeviceID: "d1"}, &coordinator.Device{ID: "d1", SignPublicKey: "key"}, signingAttached},
		{"координатор: ещё нет ключа", &settings.Settings{DeviceID: "d1"}, &coordinator.Device{ID: "d1"}, signingPending},
		{"отозвано", &settings.Settings{DeviceID: "d1", SignPublicKey: "key"},
			&coordinator.Device{ID: "d1", SignPublicKey: "key", RevokedAt: time.Now()}, signingRevoked},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := signingState(item.stored, item.device); got != item.want {
				t.Errorf("signingState = %q, want %q", got, item.want)
			}
		})
	}
}
