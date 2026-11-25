package api

import (
	"net/http"
	"testing"

	"alceacore.ru/alceamesh/internal/coordinator"
)

func TestRevokedDeviceGetsAMachineReason(t *testing.T) {
	server, registry, _, _ := newAuthServer(t, false)

	device, err := registry.Announce(coordinator.AnnounceRequest{PublicKey: "wg-revoked-live", Name: "списанный"})
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if _, err := registry.RevokeDevice(device.ID); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}

	status, payload := postJSON(t, server.URL+"/v1/presence", map[string]any{"public_key": "wg-revoked-live", "name": "снова"})
	if status != http.StatusUnauthorized {
		t.Fatalf("presence: status = %d, payload = %v", status, payload)
	}
	if got := reasonOf(t, payload); got != string(ReasonRevoked) {
		t.Errorf("presence reason = %q, want %q", got, ReasonRevoked)
	}

	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/heartbeat", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+device.Secret)
	status, payload = send(t, request)
	if status != http.StatusUnauthorized {
		t.Fatalf("heartbeat: status = %d, payload = %v", status, payload)
	}
	if got := reasonOf(t, payload); got != string(ReasonRevoked) {
		t.Errorf("heartbeat reason = %q, want %q", got, ReasonRevoked)
	}
}
