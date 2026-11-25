package sqlitestore

import (
	"database/sql"
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

func TestInvitationRoundTrip(t *testing.T) {
	store := openTestStore(t)

	created := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	invitation := coordinator.Invitation{
		ID:        "inv-1",
		CodeHash:  coordinator.HashInviteCode("abcd1234"),
		Label:     "для Димы",
		MeshIP:    "10.0.0.5",
		MaxUses:   2,
		Uses:      1,
		CreatedAt: created,
		ExpiresAt: created.Add(24 * time.Hour),
	}
	if err := store.PutInvitation(invitation); err != nil {
		t.Fatalf("PutInvitation: %v", err)
	}

	byID, ok, err := store.GetInvitation("inv-1")
	if err != nil || !ok {
		t.Fatalf("GetInvitation: ok = %v, err = %v", ok, err)
	}
	if byID.CodeHash != invitation.CodeHash || byID.Label != invitation.Label ||
		byID.MaxUses != 2 || byID.Uses != 1 || byID.MeshIP != "10.0.0.5" {
		t.Errorf("invitation = %+v", byID)
	}
	if !byID.CreatedAt.Equal(created) || !byID.ExpiresAt.Equal(invitation.ExpiresAt) {
		t.Errorf("timestamps = %s / %s", byID.CreatedAt, byID.ExpiresAt)
	}

	byHash, ok, err := store.GetInvitationByHash(invitation.CodeHash)
	if err != nil || !ok || byHash.ID != "inv-1" {
		t.Errorf("GetInvitationByHash: ok = %v, err = %v, invitation = %+v", ok, err, byHash)
	}

	invitation.Uses = 2
	invitation.RevokedAt = created.Add(time.Hour)
	if err := store.PutInvitation(invitation); err != nil {
		t.Fatalf("PutInvitation (update): %v", err)
	}
	updated, _, err := store.GetInvitation("inv-1")
	if err != nil {
		t.Fatalf("GetInvitation: %v", err)
	}
	if updated.Uses != 2 || !updated.Revoked() {
		t.Errorf("updated = %+v, want uses 2 and revoked", updated)
	}

	list, err := store.ListInvitations()
	if err != nil {
		t.Fatalf("ListInvitations: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("list length = %d, want 1", len(list))
	}

	if _, ok, err := store.GetInvitation("нет"); err != nil || ok {
		t.Errorf("unknown id: ok = %v, err = %v", ok, err)
	}
	if _, ok, err := store.GetInvitationByHash("нет"); err != nil || ok {
		t.Errorf("unknown hash: ok = %v, err = %v", ok, err)
	}
	if _, ok, err := store.GetInvitationByHash(""); err != nil || ok {
		t.Errorf("empty hash: ok = %v, err = %v", ok, err)
	}
}

func TestDeviceIdentityFieldsRoundTrip(t *testing.T) {
	store := openTestStore(t)

	enrolled := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	device := coordinator.Device{
		ID:            "dev-1",
		Secret:        "secret-1",
		Name:          "ноут",
		PublicKey:     "wg-public",
		SignPublicKey: "sign-public",
		MeshIP:        "10.0.0.7",
		Platform:      "windows",
		Available:     true,
		EnrolledAt:    enrolled,
		LastSeen:      enrolled.Add(time.Minute),
		RevokedAt:     enrolled.Add(2 * time.Hour),
		Legacy:        true,
	}
	if err := store.PutDevice(device); err != nil {
		t.Fatalf("PutDevice: %v", err)
	}

	got, ok, err := store.GetDevice("dev-1")
	if err != nil || !ok {
		t.Fatalf("GetDevice: ok = %v, err = %v", ok, err)
	}
	if got.SignPublicKey != "sign-public" {
		t.Errorf("sign key = %q", got.SignPublicKey)
	}
	if !got.Legacy {
		t.Error("the legacy flag must survive a round trip")
	}
	if !got.Revoked() || !got.RevokedAt.Equal(device.RevokedAt) {
		t.Errorf("revoked_at = %s, want %s", got.RevokedAt, device.RevokedAt)
	}
	if got.Attachable() {
		t.Error("a revoked device must not be attachable")
	}
}

func TestOpenMigratesOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	legacy, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	if _, err := legacy.Exec(`
		CREATE TABLE devices (
			id          TEXT PRIMARY KEY,
			secret      TEXT NOT NULL,
			name        TEXT NOT NULL,
			public_key  TEXT NOT NULL DEFAULT '',
			mesh_ip     TEXT NOT NULL DEFAULT '',
			platform    TEXT NOT NULL DEFAULT '',
			enrolled_at TEXT NOT NULL DEFAULT '',
			last_seen   TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		t.Fatalf("create old table: %v", err)
	}
	if _, err := legacy.Exec(`INSERT INTO devices (id, secret, name, public_key, mesh_ip, platform, enrolled_at, last_seen)
		VALUES ('old-1', 'sec-old', 'старый ПК', 'wg-old', '10.0.0.9', 'windows', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert old row: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open (migrate): %v", err)
	}
	defer store.Close()

	device, ok, err := store.GetDevice("old-1")
	if err != nil || !ok {
		t.Fatalf("the migrated device must be readable: ok = %v, err = %v", ok, err)
	}
	if device.Name != "старый ПК" || device.PublicKey != "wg-old" || device.MeshIP != "10.0.0.9" {
		t.Errorf("migrated device = %+v", device)
	}
	if device.SignPublicKey != "" || device.Revoked() || device.Legacy {
		t.Errorf("the new columns must be empty for old rows: %+v", device)
	}

	invitation := coordinator.Invitation{
		ID:        "inv-old",
		CodeHash:  coordinator.HashInviteCode("код"),
		CreatedAt: time.Now().UTC(),
	}
	if err := store.PutInvitation(invitation); err != nil {
		t.Fatalf("the invitations table must exist after migration: %v", err)
	}
	if _, ok, err := store.GetInvitationByHash(invitation.CodeHash); err != nil || !ok {
		t.Errorf("GetInvitationByHash: ok = %v, err = %v", ok, err)
	}
}
