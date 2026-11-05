package coordclient

import (
	"net/http/httptest"
	"testing"
	"time"

	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/coordinator/api"
)

func TestClientRoundTrip(t *testing.T) {
	registry, err := coordinator.NewRegistry(coordinator.NewMemoryStore(), coordinator.Options{Pool: "10.0.0.0/24"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	server := httptest.NewServer((&api.Server{Registry: registry, AdminKey: "admin", Version: "test"}).Handler())
	defer server.Close()

	admin := New(server.URL, "admin")
	if err := admin.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if version, err := admin.Version(); err != nil || version != "test" {
		t.Fatalf("Version = %q err=%v", version, err)
	}

	token, err := admin.CreateToken("laptop", "", time.Hour)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	anonymous := New(server.URL, "")
	enrolled, err := anonymous.Enroll(token.Value, "Alice", "", "windows")
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if enrolled.Device.MeshIP != "10.0.0.2" || enrolled.Secret == "" {
		t.Fatalf("unexpected enroll result: %+v", enrolled)
	}

	device := New(server.URL, enrolled.Secret)
	if err := device.Heartbeat(); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}

	devices, err := device.Devices()
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devices) != 1 || !devices[0].Online {
		t.Fatalf("unexpected devices: %+v", devices)
	}

	if err := admin.DeleteDevice(enrolled.Device.ID); err != nil {
		t.Fatalf("DeleteDevice: %v", err)
	}
	if remaining, _ := admin.Devices(); len(remaining) != 0 {
		t.Fatalf("expected no devices, got %d", len(remaining))
	}
}
