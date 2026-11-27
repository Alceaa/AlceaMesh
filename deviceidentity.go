package main

import (
	"crypto/ecdh"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"alceacore.ru/alceamesh/internal/coordclient"
	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/datadir"
	"alceacore.ru/alceamesh/internal/identity"
	"alceacore.ru/alceamesh/internal/settings"
	"alceacore.ru/alceamesh/internal/trust"
	"alceacore.ru/alceamesh/internal/wgconf"
)

func deviceIdentityDir() string {
	return filepath.Join(datadir.Dir(), "identity")
}

func deviceKey() (*identity.Key, error) {
	return identity.LoadOrCreate(deviceIdentityDir())
}

func meshAddress() string {
	if custom := strings.TrimSpace(os.Getenv("ALCEAMESH_MESH_ADDRESS")); custom != "" {
		return custom
	}
	return trust.MeshAddress
}

func coordinatorEndpoints() []coordclient.Endpoint {
	if custom := strings.TrimSpace(os.Getenv("ALCEAMESH_COORDINATOR")); custom != "" {
		return []coordclient.Endpoint{{Name: "custom", BaseURL: custom}}
	}
	return []coordclient.Endpoint{
		{Name: "mesh", BaseURL: "https://" + meshAddress(), ServerName: trust.ServerName},
		{Name: "public", BaseURL: "https://" + trust.ServerName, ServerName: trust.ServerName},
	}
}

func newCoordinatorClient(signer coordclient.Signer) (*coordclient.Client, error) {
	options := coordclient.Options{
		Endpoints: coordinatorEndpoints(),
		Signer:    signer,
		Timeout:   12 * time.Second,
	}
	if config, err := trust.TLSConfig(); err == nil {
		options.TLS = config
	}
	return coordclient.NewClient(options)
}

func deviceCredentials(stored *settings.Settings) coordclient.Signer {
	if stored == nil || strings.TrimSpace(stored.DeviceID) == "" {
		return nil
	}
	key, err := deviceKey()
	if err != nil {
		return nil
	}
	return identity.Credentials{DeviceID: stored.DeviceID, Key: key}
}

func wireGuardKeys(stored *settings.Settings) (*wgconf.Config, *ecdh.PrivateKey, string, error) {
	if stored == nil || strings.TrimSpace(stored.ConfigText) == "" {
		return nil, nil, "", errors.New("конфигурация WireGuard не импортирована")
	}
	config, err := wgconf.Parse([]byte(stored.ConfigText))
	if err != nil {
		return nil, nil, "", err
	}
	private, err := config.PrivateKey()
	if err != nil {
		return nil, nil, "", err
	}
	public, err := config.PublicKey()
	if err != nil {
		return nil, nil, "", err
	}
	return config, private, public, nil
}

func attachDevice(client *coordclient.Client, stored *settings.Settings, key *identity.Key) (*coordinator.Device, error) {
	_, private, public, err := wireGuardKeys(stored)
	if err != nil {
		return nil, err
	}

	challenge, err := client.ClaimChallenge(public)
	if err != nil {
		return nil, err
	}
	raw, err := identity.DecodeToken(challenge.Challenge)
	if err != nil {
		return nil, err
	}
	shared, err := identity.SharedSecret(private, challenge.ServerKey)
	if err != nil {
		return nil, err
	}

	proof := identity.EncodeToken(identity.Proof(shared, raw))
	signature := identity.SignMessage(key, identity.CanonicalClaim(challenge.ClaimID, proof, key.PublicKey()))
	return client.Claim(challenge.ClaimID, proof, key.PublicKey(), signature)
}
