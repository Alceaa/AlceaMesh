package sqlitestore

import (
	"path/filepath"
	"testing"
	"time"

	"alceacore.ru/alceamesh/internal/coordinator"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestDeviceRoundTrip(t *testing.T) {
	store := openTestStore(t)
	now := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)

	device := coordinator.Device{
		ID: "id1", Secret: "sec1", Name: "Alice", PublicKey: "key",
		MeshIP: "10.0.0.2", Platform: "windows", EnrolledAt: now, LastSeen: now,
	}
	if err := store.PutDevice(device); err != nil {
		t.Fatalf("PutDevice: %v", err)
	}

	got, ok, err := store.GetDevice("id1")
	if err != nil || !ok {
		t.Fatalf("GetDevice: ok=%v err=%v", ok, err)
	}
	if got.Name != "Alice" || got.Secret != "sec1" || got.MeshIP != "10.0.0.2" {
		t.Errorf("unexpected device: %+v", got)
	}
	if !got.LastSeen.Equal(now) {
		t.Errorf("LastSeen = %v, want %v", got.LastSeen, now)
	}

	bySecret, ok, err := store.GetDeviceBySecret("sec1")
	if err != nil || !ok || bySecret.ID != "id1" {
		t.Fatalf("GetDeviceBySecret: ok=%v err=%v device=%+v", ok, err, bySecret)
	}

	device.Name = "Alice 2"
	if err := store.PutDevice(device); err != nil {
		t.Fatalf("PutDevice update: %v", err)
	}
	list, err := store.ListDevices()
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Alice 2" {
		t.Errorf("ListDevices = %+v", list)
	}

	if err := store.DeleteDevice("id1"); err != nil {
		t.Fatalf("DeleteDevice: %v", err)
	}
	if _, ok, _ := store.GetDevice("id1"); ok {
		t.Error("device should be gone after DeleteDevice")
	}
}

func TestTokenRoundTrip(t *testing.T) {
	store := openTestStore(t)
	now := time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)

	token := coordinator.Token{
		Value: "tok", Label: "l", MeshIP: "10.0.0.9",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := store.PutToken(token); err != nil {
		t.Fatalf("PutToken: %v", err)
	}

	got, ok, err := store.GetToken("tok")
	if err != nil || !ok {
		t.Fatalf("GetToken: ok=%v err=%v", ok, err)
	}
	if got.Label != "l" || got.MeshIP != "10.0.0.9" || !got.ExpiresAt.Equal(token.ExpiresAt) {
		t.Errorf("unexpected token: %+v", got)
	}

	token.UsedBy = "id1"
	if err := store.PutToken(token); err != nil {
		t.Fatalf("PutToken update: %v", err)
	}
	got, _, _ = store.GetToken("tok")
	if got.UsedBy != "id1" {
		t.Errorf("UsedBy = %q, want id1", got.UsedBy)
	}

	list, err := store.ListTokens()
	if err != nil || len(list) != 1 {
		t.Fatalf("ListTokens = %+v err=%v", list, err)
	}
}

func TestMissingRecords(t *testing.T) {
	store := openTestStore(t)
	if _, ok, err := store.GetDevice("nope"); ok || err != nil {
		t.Errorf("GetDevice missing: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.GetToken("nope"); ok || err != nil {
		t.Errorf("GetToken missing: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.GetDeviceBySecret(""); ok || err != nil {
		t.Errorf("GetDeviceBySecret empty: ok=%v err=%v", ok, err)
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persist.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := store.PutDevice(coordinator.Device{ID: "id1", Secret: "s", Name: "N"}); err != nil {
		t.Fatalf("PutDevice: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	got, ok, err := reopened.GetDevice("id1")
	if err != nil || !ok || got.Name != "N" {
		t.Fatalf("after reopen: ok=%v err=%v device=%+v", ok, err, got)
	}
}

func TestRegistryWithSQLite(t *testing.T) {
	store := openTestStore(t)
	registry, err := coordinator.NewRegistry(store, coordinator.Options{Pool: "10.0.0.0/24"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	token, err := registry.CreateToken("laptop", "", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	device, err := registry.Enroll(coordinator.EnrollRequest{Token: token.Value, Name: "Alice", Platform: "windows"})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if device.MeshIP != "10.0.0.2" {
		t.Fatalf("MeshIP = %q, want 10.0.0.2", device.MeshIP)
	}

	views, err := registry.Devices()
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(views) != 1 || !views[0].Online {
		t.Fatalf("unexpected devices: %+v", views)
	}
	if _, err := registry.Heartbeat(device.Secret); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
}
