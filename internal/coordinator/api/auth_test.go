package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/identity"
)

func newAuthServer(t *testing.T, requireSignature bool) (*httptest.Server, *coordinator.Registry, *coordinator.Device, *identity.Key) {
	t.Helper()

	registry, err := coordinator.NewRegistry(coordinator.NewMemoryStore(), coordinator.Options{
		Pool:        "10.0.0.0/24",
		PresenceTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	key, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}
	_, code, err := registry.CreateInvite("тест", "", 1, 0)
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	device, err := registry.EnrollWithInvite(coordinator.InviteEnrollRequest{
		Code:          code,
		Name:          "тестовый ПК",
		PublicKey:     "wg-public",
		SignPublicKey: key.PublicKey(),
	})
	if err != nil {
		t.Fatalf("EnrollWithInvite: %v", err)
	}

	server := httptest.NewServer((&Server{
		Registry:         registry,
		AdminKey:         testAdminKey,
		Version:          "test",
		RequireSignature: requireSignature,
	}).Handler())
	t.Cleanup(server.Close)
	return server, registry, device, key
}

func signedRequest(t *testing.T, key *identity.Key, deviceID, method, url string, body []byte, at time.Time) *http.Request {
	t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if err := key.SignRequest(request, body, deviceID, at); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	return request
}

func send(t *testing.T, request *http.Request) (int, map[string]any) {
	t.Helper()

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer response.Body.Close()

	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var payload map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode %q: %v", raw, err)
		}
	}
	return response.StatusCode, payload
}

func reasonOf(t *testing.T, payload map[string]any) string {
	t.Helper()
	value, ok := payload["reason"].(string)
	if !ok {
		t.Fatalf("no machine reason in %v", payload)
	}
	return value
}

func TestSignedRequestIsAccepted(t *testing.T) {
	server, _, device, key := newAuthServer(t, true)

	body := []byte(`{"public_key":"wg-public","name":"тестовый ПК","available":true}`)
	status, payload := send(t, signedRequest(t, key, device.ID, http.MethodPost, server.URL+"/v1/presence", body, time.Now()))
	if status != http.StatusOK {
		t.Fatalf("status = %d, payload = %v", status, payload)
	}
	if _, ok := payload["device"]; !ok {
		t.Errorf("payload = %v", payload)
	}
}

func TestSignedRequestFailures(t *testing.T) {
	server, registry, device, key := newAuthServer(t, true)
	body := []byte(`{"public_key":"wg-public","name":"тестовый ПК"}`)

	other, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}

	cases := []struct {
		name   string
		expect string
		build  func() *http.Request
	}{
		{
			name:   "foreign key",
			expect: string(ReasonBadSignature),
			build: func() *http.Request {
				return signedRequest(t, other, device.ID, http.MethodPost, server.URL+"/v1/presence", body, time.Now())
			},
		},
		{
			name:   "stale timestamp",
			expect: string(ReasonStale),
			build: func() *http.Request {
				return signedRequest(t, key, device.ID, http.MethodPost, server.URL+"/v1/presence", body, time.Now().Add(-10*time.Minute))
			},
		},
		{
			name:   "future timestamp",
			expect: string(ReasonStale),
			build: func() *http.Request {
				return signedRequest(t, key, device.ID, http.MethodPost, server.URL+"/v1/presence", body, time.Now().Add(10*time.Minute))
			},
		},
		{
			name:   "tampered body",
			expect: string(ReasonBadSignature),
			build: func() *http.Request {
				signed := signedRequest(t, key, device.ID, http.MethodPost, server.URL+"/v1/presence", body, time.Now())
				request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/presence",
					bytes.NewReader([]byte(`{"public_key":"подмена"}`)))
				if err != nil {
					t.Fatalf("NewRequest: %v", err)
				}
				request.Header.Set("Content-Type", "application/json")
				for _, name := range []string{
					identity.HeaderDevice, identity.HeaderTimestamp,
					identity.HeaderNonce, identity.HeaderSignature,
				} {
					request.Header.Set(name, signed.Header.Get(name))
				}
				return request
			},
		},
		{
			name:   "unknown device",
			expect: string(ReasonUnknownDevice),
			build: func() *http.Request {
				return signedRequest(t, key, "нет-такого", http.MethodPost, server.URL+"/v1/presence", body, time.Now())
			},
		},
		{
			name:   "incomplete headers",
			expect: string(ReasonMalformed),
			build: func() *http.Request {
				request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/presence", bytes.NewReader(body))
				if err != nil {
					t.Fatalf("NewRequest: %v", err)
				}
				request.Header.Set(identity.HeaderDevice, device.ID)
				return request
			},
		},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			status, payload := send(t, item.build())
			if status != http.StatusUnauthorized {
				t.Fatalf("status = %d, payload = %v", status, payload)
			}
			if got := reasonOf(t, payload); got != item.expect {
				t.Errorf("reason = %q, want %q", got, item.expect)
			}
		})
	}

	if _, err := registry.RevokeDevice(device.ID); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}
	status, payload := send(t, signedRequest(t, key, device.ID, http.MethodPost, server.URL+"/v1/presence", body, time.Now()))
	if status != http.StatusUnauthorized || reasonOf(t, payload) != string(ReasonRevoked) {
		t.Errorf("revoked: status = %d, payload = %v", status, payload)
	}
}

func TestReplayIsRejected(t *testing.T) {
	server, _, device, key := newAuthServer(t, true)
	body := []byte(`{"public_key":"wg-public","name":"тестовый ПК"}`)

	request := signedRequest(t, key, device.ID, http.MethodPost, server.URL+"/v1/presence", body, time.Now())
	status, payload := send(t, request)
	if status != http.StatusOK {
		t.Fatalf("first request: status = %d, payload = %v", status, payload)
	}

	status, payload = send(t, request)
	if status != http.StatusUnauthorized {
		t.Fatalf("replay: status = %d", status)
	}
	if got := reasonOf(t, payload); got != string(ReasonReplay) {
		t.Errorf("reason = %q, want %q", got, ReasonReplay)
	}
}

func TestDeviceWithoutSignKeyIsRejected(t *testing.T) {
	server, registry, _, key := newAuthServer(t, true)

	old, err := registry.Announce(coordinator.AnnounceRequest{PublicKey: "wg-old", Name: "старый"})
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}

	body := []byte(`{"public_key":"wg-old","name":"старый"}`)
	status, payload := send(t, signedRequest(t, key, old.ID, http.MethodPost, server.URL+"/v1/presence", body, time.Now()))
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, payload = %v", status, payload)
	}
	if got := reasonOf(t, payload); got != string(ReasonUnknownDevice) {
		t.Errorf("reason = %q, want %q", got, ReasonUnknownDevice)
	}
}

func TestSignatureRequirementAndLegacyMode(t *testing.T) {
	strict, _, _, _ := newAuthServer(t, true)
	legacy, _, _, _ := newAuthServer(t, false)

	body := []byte(`{"public_key":"wg-legacy","name":"старый клиент"}`)

	response, err := http.Post(strict.URL+"/v1/presence", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	raw, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("strict mode must reject unsigned requests: status = %d, body = %s", response.StatusCode, raw)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := payload["reason"]; got != string(ReasonSignatureRequired) {
		t.Errorf("reason = %v, want %q", got, ReasonSignatureRequired)
	}

	response, err = http.Post(legacy.URL+"/v1/presence", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	raw, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("legacy mode must accept unsigned requests: status = %d, body = %s", response.StatusCode, raw)
	}
}

func TestAnonymousAndAdminRequests(t *testing.T) {
	server, _, device, key := newAuthServer(t, true)

	for _, path := range []string{"/v1/ping", "/v1/version"} {
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Errorf("%s must stay anonymous: status = %d", path, response.StatusCode)
		}
	}

	request, err := http.NewRequest(http.MethodGet, server.URL+"/v1/devices", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+testAdminKey)
	status, payload := send(t, request)
	if status != http.StatusOK {
		t.Errorf("the admin key must work without a signature: status = %d, payload = %v", status, payload)
	}

	request, err = http.NewRequest(http.MethodGet, server.URL+"/v1/devices", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	status, payload = send(t, request)
	if status != http.StatusUnauthorized {
		t.Errorf("an unsigned request must be rejected: status = %d, payload = %v", status, payload)
	}
	if got := reasonOf(t, payload); got != string(ReasonSignatureRequired) {
		t.Errorf("reason = %q, want %q", got, ReasonSignatureRequired)
	}

	request, err = http.NewRequest(http.MethodGet, server.URL+"/v1/peers", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := key.SignRequest(request, nil, device.ID, time.Now()); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	status, payload = send(t, request)
	if status != http.StatusOK {
		t.Errorf("a signed device request must be accepted: status = %d, payload = %v", status, payload)
	}

	request, err = http.NewRequest(http.MethodGet, server.URL+"/v1/devices", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := key.SignRequest(request, nil, device.ID, time.Now()); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	status, _ = send(t, request)
	if status != http.StatusUnauthorized {
		t.Errorf("the admin endpoint must keep requiring the admin key: status = %d", status)
	}
}
