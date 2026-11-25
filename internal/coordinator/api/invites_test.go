package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/identity"
)

func postJSONAuth(t *testing.T, url, adminKey string, payload any) (int, map[string]any) {
	t.Helper()

	var reader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(http.MethodPost, url, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if adminKey != "" {
		request.Header.Set("Authorization", "Bearer "+adminKey)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer response.Body.Close()

	var decoded map[string]any
	raw, _ := io.ReadAll(response.Body)
	_ = json.Unmarshal(raw, &decoded)
	return response.StatusCode, decoded
}

func getJSONAuth(t *testing.T, url, adminKey string) (int, map[string]any) {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if adminKey != "" {
		request.Header.Set("Authorization", "Bearer "+adminKey)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	return response.StatusCode, decoded
}

func TestInviteAdminFlow(t *testing.T) {
	server, _, _, _ := newAuthServer(t, false)

	status, payload := postJSON(t, server.URL+"/v1/invites", map[string]any{"label": "без ключа"})
	if status != http.StatusUnauthorized {
		t.Fatalf("an invitation without the admin key must be rejected: status = %d, payload = %v", status, payload)
	}

	status, payload = postJSONAuth(t, server.URL+"/v1/invites", testAdminKey, map[string]any{
		"label":       "для Димы",
		"uses":        2,
		"ttl_seconds": 3600,
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d, payload = %v", status, payload)
	}
	code, _ := payload["code"].(string)
	if code == "" {
		t.Fatalf("no invitation code in %v", payload)
	}
	invitation, ok := payload["invitation"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %v", payload)
	}
	id, _ := invitation["id"].(string)
	if id == "" || invitation["label"] != "для Димы" {
		t.Errorf("invitation = %v", invitation)
	}
	if _, leaked := invitation["code_hash"]; leaked {
		t.Error("the code hash must never leave the server")
	}

	status, payload = getJSONAuth(t, server.URL+"/v1/invites", testAdminKey)
	if status != http.StatusOK {
		t.Fatalf("list: status = %d, payload = %v", status, payload)
	}
	invitations, _ := payload["invitations"].([]any)
	found := false
	for _, item := range invitations {
		entry, _ := item.(map[string]any)
		if entry["id"] == id {
			found = true
			if entry["label"] != "для Димы" || entry["max_uses"] != float64(2) {
				t.Errorf("invitation = %v", entry)
			}
		}
	}
	if !found {
		t.Errorf("invitations = %v, want the one with id %s", invitations, id)
	}

	status, payload = postJSONAuth(t, server.URL+"/v1/invites/"+id+"/revoke", testAdminKey, nil)
	if status != http.StatusOK {
		t.Fatalf("revoke: status = %d, payload = %v", status, payload)
	}
	revoked, _ := payload["invitation"].(map[string]any)
	if revoked["id"] != id {
		t.Errorf("revoked = %v", revoked)
	}

	status, payload = postJSONAuth(t, server.URL+"/v1/invites/нет-такого/revoke", testAdminKey, nil)
	if status != http.StatusUnauthorized || payload["reason"] != string(ReasonInviteUnknown) {
		t.Errorf("unknown invitation: status = %d, payload = %v", status, payload)
	}
}

func enrollBody(code, name, platform, publicKey string, key *identity.Key) map[string]string {
	payload := identity.CanonicalEnroll(code, name, platform, publicKey, key.PublicKey())
	return map[string]string{
		"invite":          code,
		"name":            name,
		"platform":        platform,
		"public_key":      publicKey,
		"sign_public_key": key.PublicKey(),
		"signature":       identity.SignMessage(key, payload),
	}
}

func TestEnrollByInvitation(t *testing.T) {
	server, registry, _, _ := newAuthServer(t, false)

	_, code, err := registry.CreateInvite("Дима", "", 1, time.Hour)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	key, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}

	status, payload := postJSON(t, server.URL+"/v1/enroll", enrollBody(code, "Димин ноут", "windows", "wg-dima", key))
	if status != http.StatusCreated {
		t.Fatalf("status = %d, payload = %v", status, payload)
	}
	device, ok := payload["device"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %v", payload)
	}
	if device["sign_public_key"] != key.PublicKey() {
		t.Errorf("sign key = %v, want %v", device["sign_public_key"], key.PublicKey())
	}
	if _, hasSecret := payload["secret"]; !hasSecret {
		t.Error("the device secret must be returned once")
	}
	if legacy, ok := device["legacy"]; ok && legacy != false {
		t.Errorf("an invited device must not be legacy: %v", legacy)
	}

	status, payload = postJSON(t, server.URL+"/v1/enroll", enrollBody(code, "Второй", "windows", "wg-second", key))
	if status != http.StatusUnauthorized || payload["reason"] != string(ReasonInviteExhausted) {
		t.Errorf("reused invite: status = %d, payload = %v", status, payload)
	}

	status, payload = postJSON(t, server.URL+"/v1/enroll", enrollBody("ffffffffffffffffffffffffffffffff", "Чужой", "windows", "wg-nobody", key))
	if status != http.StatusUnauthorized || payload["reason"] != string(ReasonInviteUnknown) {
		t.Errorf("unknown invite: status = %d, payload = %v", status, payload)
	}

	other, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}
	_, secondCode, err := registry.CreateInvite("Ещё", "", 1, 0)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	tampered := enrollBody(secondCode, "Подмена", "windows", "wg-x", other)
	tampered["signature"] = identity.SignMessage(other,
		identity.CanonicalEnroll(secondCode, "Подмена", "windows", "другой", other.PublicKey()))
	status, payload = postJSON(t, server.URL+"/v1/enroll", tampered)
	if status != http.StatusUnauthorized || payload["reason"] != string(ReasonBadSignKey) {
		t.Errorf("tampered enrollment: status = %d, payload = %v", status, payload)
	}
}

func TestDevicesPendingFilter(t *testing.T) {
	server, registry, _, _ := newAuthServer(t, false)

	old, err := registry.Announce(coordinator.AnnounceRequest{PublicKey: "wg-legacy", Name: "старый"})
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}

	status, payload := getJSONAuth(t, server.URL+"/v1/devices?pending=1", testAdminKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d, payload = %v", status, payload)
	}
	devices, _ := payload["devices"].([]any)
	if len(devices) != 1 {
		t.Fatalf("devices = %v, want one pending device", payload["devices"])
	}
	first, _ := devices[0].(map[string]any)
	if first["id"] != old.ID {
		t.Errorf("pending device = %v, want %s", first["id"], old.ID)
	}

	key, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}
	if _, err := registry.AttachDevice(old.ID, key.PublicKey()); err != nil {
		t.Fatalf("AttachDevice: %v", err)
	}
	status, payload = getJSONAuth(t, server.URL+"/v1/devices?pending=1", testAdminKey)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if devices, _ := payload["devices"].([]any); len(devices) != 0 {
		t.Errorf("devices = %v, want none after attaching", payload["devices"])
	}
}
