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
)

const testAdminKey = "admin-secret"

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	registry, err := coordinator.NewRegistry(coordinator.NewMemoryStore(), coordinator.Options{
		Pool:        "10.0.0.0/24",
		PresenceTTL: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	server := httptest.NewServer((&Server{
		Registry: registry,
		AdminKey: testAdminKey,
		Version:  "test",
	}).Handler())
	t.Cleanup(server.Close)
	return server
}

func doJSON(t *testing.T, method, url, key string, body any) (*http.Response, []byte) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, data
}

func TestPingAndVersion(t *testing.T) {
	server := newTestServer(t)

	resp, data := doJSON(t, http.MethodGet, server.URL+"/v1/ping", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ping status = %d", resp.StatusCode)
	}
	var ping map[string]bool
	if err := json.Unmarshal(data, &ping); err != nil || !ping["pong"] {
		t.Fatalf("ping body = %s", data)
	}

	resp, data = doJSON(t, http.MethodGet, server.URL+"/v1/version", "", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(data, []byte("test")) {
		t.Fatalf("version status = %d body = %s", resp.StatusCode, data)
	}
}

func TestEnrollHeartbeatAndDevices(t *testing.T) {
	server := newTestServer(t)

	resp, data := doJSON(t, http.MethodPost, server.URL+"/v1/tokens", testAdminKey,
		map[string]any{"label": "laptop", "ttl_seconds": 3600})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create token status = %d body = %s", resp.StatusCode, data)
	}
	var tokenOut struct {
		Token coordinator.Token `json:"token"`
	}
	if err := json.Unmarshal(data, &tokenOut); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if tokenOut.Token.Value == "" {
		t.Fatal("empty token value")
	}

	resp, data = doJSON(t, http.MethodPost, server.URL+"/v1/enroll", "",
		map[string]string{"token": tokenOut.Token.Value, "name": "Alice", "platform": "windows"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("enroll status = %d body = %s", resp.StatusCode, data)
	}
	var enrollOut enrollResponse
	if err := json.Unmarshal(data, &enrollOut); err != nil {
		t.Fatalf("decode enroll: %v", err)
	}
	if enrollOut.Secret == "" || enrollOut.Device.MeshIP != "10.0.0.2" {
		t.Fatalf("unexpected enroll response: %+v", enrollOut)
	}
	if enrollOut.Device.Secret != "" {
		t.Error("device secret must not be serialized inside the device object")
	}

	resp, data = doJSON(t, http.MethodPost, server.URL+"/v1/heartbeat", enrollOut.Secret, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("heartbeat status = %d body = %s", resp.StatusCode, data)
	}

	resp, data = doJSON(t, http.MethodGet, server.URL+"/v1/devices", enrollOut.Secret, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("devices status = %d body = %s", resp.StatusCode, data)
	}
	var listOut struct {
		Devices []coordinator.DeviceView `json:"devices"`
	}
	if err := json.Unmarshal(data, &listOut); err != nil {
		t.Fatalf("decode devices: %v", err)
	}
	if len(listOut.Devices) != 1 || !listOut.Devices[0].Online {
		t.Fatalf("unexpected devices: %+v", listOut.Devices)
	}

	resp, _ = doJSON(t, http.MethodPost, server.URL+"/v1/enroll", "",
		map[string]string{"token": tokenOut.Token.Value, "name": "Bob"})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("reused token status = %d, want 403", resp.StatusCode)
	}
}

func TestEndpointsRequireAuth(t *testing.T) {
	server := newTestServer(t)

	if resp, _ := doJSON(t, http.MethodGet, server.URL+"/v1/devices", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("devices without auth = %d, want 401", resp.StatusCode)
	}
	if resp, _ := doJSON(t, http.MethodGet, server.URL+"/v1/devices", "bogus", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("devices with bad auth = %d, want 401", resp.StatusCode)
	}
	if resp, _ := doJSON(t, http.MethodPost, server.URL+"/v1/tokens", "", map[string]any{}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("tokens without admin = %d, want 401", resp.StatusCode)
	}
	if resp, _ := doJSON(t, http.MethodPost, server.URL+"/v1/heartbeat", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("heartbeat without secret = %d, want 401", resp.StatusCode)
	}
}

func TestDeleteDevice(t *testing.T) {
	server := newTestServer(t)

	_, data := doJSON(t, http.MethodPost, server.URL+"/v1/tokens", testAdminKey, map[string]any{})
	var tokenOut struct {
		Token coordinator.Token `json:"token"`
	}
	json.Unmarshal(data, &tokenOut)

	_, data = doJSON(t, http.MethodPost, server.URL+"/v1/enroll", "",
		map[string]string{"token": tokenOut.Token.Value, "name": "Alice"})
	var enrollOut enrollResponse
	json.Unmarshal(data, &enrollOut)

	url := server.URL + "/v1/devices/" + enrollOut.Device.ID
	if resp, _ := doJSON(t, http.MethodDelete, url, "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("delete without admin = %d, want 401", resp.StatusCode)
	}
	if resp, _ := doJSON(t, http.MethodDelete, url, testAdminKey, nil); resp.StatusCode != http.StatusOK {
		t.Errorf("delete with admin = %d, want 200", resp.StatusCode)
	}
}
