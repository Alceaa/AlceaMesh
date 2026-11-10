package coordinator

import (
	"reflect"
	"testing"
	"time"
)

type clock struct {
	t time.Time
}

func (c *clock) now() time.Time {
	return c.t
}

func newTestRegistry(t *testing.T) (*Registry, *clock) {
	t.Helper()
	clk := &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	registry, err := NewRegistry(NewMemoryStore(), Options{
		Pool:        "10.0.0.0/24",
		PresenceTTL: 60 * time.Second,
		Now:         clk.now,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry, clk
}

func TestEnrollAssignsMeshIPAndMarksTokenUsed(t *testing.T) {
	registry, _ := newTestRegistry(t)

	token, err := registry.CreateToken("laptop", "", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	device, err := registry.Enroll(EnrollRequest{Token: token.Value, Name: "Alice laptop", Platform: "windows"})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if device.MeshIP != "10.0.0.2" {
		t.Errorf("MeshIP = %q, want 10.0.0.2", device.MeshIP)
	}
	if device.ID == "" || device.Secret == "" {
		t.Errorf("device id and secret must be generated: %+v", device)
	}
	if device.Name != "Alice laptop" || device.Platform != "windows" {
		t.Errorf("unexpected device: %+v", device)
	}

	tokens, err := registry.Tokens()
	if err != nil {
		t.Fatalf("Tokens: %v", err)
	}
	if len(tokens) != 1 || tokens[0].UsedBy != device.ID {
		t.Errorf("token should be marked as used by %q: %+v", device.ID, tokens)
	}
}

func TestEnrollRejectsUnknownToken(t *testing.T) {
	registry, _ := newTestRegistry(t)
	if _, err := registry.Enroll(EnrollRequest{Token: "nope", Name: "x"}); err == nil {
		t.Fatal("expected an error for an unknown token")
	}
}

func TestEnrollRejectsUsedToken(t *testing.T) {
	registry, _ := newTestRegistry(t)
	token, _ := registry.CreateToken("", "", 0)

	if _, err := registry.Enroll(EnrollRequest{Token: token.Value, Name: "one"}); err != nil {
		t.Fatalf("first enroll: %v", err)
	}
	if _, err := registry.Enroll(EnrollRequest{Token: token.Value, Name: "two"}); err == nil {
		t.Fatal("expected an error for a reused token")
	}
}

func TestEnrollRejectsExpiredToken(t *testing.T) {
	registry, clk := newTestRegistry(t)
	token, err := registry.CreateToken("", "", 30*time.Minute)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	clk.t = clk.t.Add(31 * time.Minute)
	if _, err := registry.Enroll(EnrollRequest{Token: token.Value, Name: "late"}); err == nil {
		t.Fatal("expected an error for an expired token")
	}
}

func TestEnrollRequiresName(t *testing.T) {
	registry, _ := newTestRegistry(t)
	token, _ := registry.CreateToken("", "", 0)
	if _, err := registry.Enroll(EnrollRequest{Token: token.Value, Name: "   "}); err == nil {
		t.Fatal("expected an error for an empty device name")
	}
}

func TestEnrollHonorsRequestedMeshIP(t *testing.T) {
	registry, _ := newTestRegistry(t)
	token, err := registry.CreateToken("", "10.0.0.42", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	device, err := registry.Enroll(EnrollRequest{Token: token.Value, Name: "pinned"})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if device.MeshIP != "10.0.0.42" {
		t.Errorf("MeshIP = %q, want 10.0.0.42", device.MeshIP)
	}
}

func TestEnrollRejectsTakenMeshIP(t *testing.T) {
	registry, _ := newTestRegistry(t)

	first, _ := registry.CreateToken("", "10.0.0.42", 0)
	if _, err := registry.Enroll(EnrollRequest{Token: first.Value, Name: "one"}); err != nil {
		t.Fatalf("first enroll: %v", err)
	}

	second, _ := registry.CreateToken("", "10.0.0.42", 0)
	if _, err := registry.Enroll(EnrollRequest{Token: second.Value, Name: "two"}); err == nil {
		t.Fatal("expected an error for a duplicate mesh ip")
	}
}

func TestCreateTokenRejectsInvalidMeshIP(t *testing.T) {
	registry, _ := newTestRegistry(t)
	if _, err := registry.CreateToken("", "not-an-ip", 0); err == nil {
		t.Fatal("expected an error for an invalid mesh ip")
	}
}

func TestPresenceAndHeartbeat(t *testing.T) {
	registry, clk := newTestRegistry(t)
	token, _ := registry.CreateToken("", "", 0)
	device, err := registry.Enroll(EnrollRequest{Token: token.Value, Name: "phone"})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	views, err := registry.Devices()
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(views) != 1 || !views[0].Online {
		t.Fatalf("device should be online right after enrollment: %+v", views)
	}

	clk.t = clk.t.Add(61 * time.Second)
	views, _ = registry.Devices()
	if views[0].Online {
		t.Error("device should be offline after the presence ttl")
	}

	if _, err := registry.Heartbeat(device.Secret); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	views, _ = registry.Devices()
	if !views[0].Online {
		t.Error("device should be online again after a heartbeat")
	}
}

func TestHeartbeatRejectsUnknownSecret(t *testing.T) {
	registry, _ := newTestRegistry(t)
	if _, err := registry.Heartbeat("bogus"); err == nil {
		t.Fatal("expected an error for an unknown secret")
	}
}

func TestNextFreeIPSkipsGatewayAndUsed(t *testing.T) {
	registry, _ := newTestRegistry(t)

	var ips []string
	for i := 0; i < 3; i++ {
		token, _ := registry.CreateToken("", "", 0)
		device, err := registry.Enroll(EnrollRequest{Token: token.Value, Name: "dev"})
		if err != nil {
			t.Fatalf("enroll %d: %v", i, err)
		}
		ips = append(ips, device.MeshIP)
	}

	want := []string{"10.0.0.2", "10.0.0.3", "10.0.0.4"}
	if !reflect.DeepEqual(ips, want) {
		t.Errorf("ips = %v, want %v", ips, want)
	}
}

func TestPoolExhaustion(t *testing.T) {
	registry, err := NewRegistry(NewMemoryStore(), Options{Pool: "10.9.9.0/30"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	first, _ := registry.CreateToken("", "", 0)
	if _, err := registry.Enroll(EnrollRequest{Token: first.Value, Name: "a"}); err != nil {
		t.Fatalf("first enroll: %v", err)
	}

	second, _ := registry.CreateToken("", "", 0)
	if _, err := registry.Enroll(EnrollRequest{Token: second.Value, Name: "b"}); err == nil {
		t.Fatal("expected a pool exhaustion error")
	}
}

func TestDeleteDevice(t *testing.T) {
	registry, _ := newTestRegistry(t)
	token, _ := registry.CreateToken("", "", 0)
	device, err := registry.Enroll(EnrollRequest{Token: token.Value, Name: "tmp"})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	if err := registry.DeleteDevice(device.ID); err != nil {
		t.Fatalf("DeleteDevice: %v", err)
	}
	views, _ := registry.Devices()
	if len(views) != 0 {
		t.Errorf("expected no devices, got %d", len(views))
	}
	if _, err := registry.Heartbeat(device.Secret); err == nil {
		t.Error("heartbeat should fail after the device is deleted")
	}
}

func TestAnnounceUpsertsByPublicKey(t *testing.T) {
	registry, clk := newTestRegistry(t)

	first, err := registry.Announce(AnnounceRequest{
		PublicKey: "puba", Name: "Alice", MeshIP: "10.0.0.5", Platform: "windows", Available: true,
	})
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if first.MeshIP != "10.0.0.5" || !first.Available || first.ID == "" {
		t.Fatalf("unexpected device: %+v", first)
	}

	clk.t = clk.t.Add(time.Minute)
	second, err := registry.Announce(AnnounceRequest{
		PublicKey: "puba", Name: "Alice 2", MeshIP: "10.0.0.5", Available: false,
	})
	if err != nil {
		t.Fatalf("Announce 2: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("device id changed: %s != %s", second.ID, first.ID)
	}
	if second.Name != "Alice 2" || second.Available {
		t.Errorf("announce did not update the device: %+v", second)
	}

	devices, _ := registry.Devices()
	if len(devices) != 1 {
		t.Errorf("expected 1 device, got %d", len(devices))
	}
}

func TestAnnounceRequiresPublicKey(t *testing.T) {
	registry, _ := newTestRegistry(t)
	if _, err := registry.Announce(AnnounceRequest{Name: "x"}); err == nil {
		t.Fatal("expected an error for a missing public key")
	}
}

func TestAnnounceKeepsMeshIP(t *testing.T) {
	registry, _ := newTestRegistry(t)

	first, err := registry.Announce(AnnounceRequest{PublicKey: "p1", Name: "A"})
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	second, err := registry.Announce(AnnounceRequest{PublicKey: "p1", Name: "A"})
	if err != nil {
		t.Fatalf("Announce 2: %v", err)
	}
	if first.MeshIP != second.MeshIP {
		t.Errorf("mesh ip changed: %s != %s", first.MeshIP, second.MeshIP)
	}
}

func TestPeersFiltersByAvailability(t *testing.T) {
	registry, _ := newTestRegistry(t)

	if _, err := registry.Announce(AnnounceRequest{PublicKey: "p1", Name: "Ready", Available: true}); err != nil {
		t.Fatalf("Announce p1: %v", err)
	}
	if _, err := registry.Announce(AnnounceRequest{PublicKey: "p2", Name: "Busy"}); err != nil {
		t.Fatalf("Announce p2: %v", err)
	}

	all, err := registry.Peers(false)
	if err != nil {
		t.Fatalf("Peers(false): %v", err)
	}
	if len(all) != 2 {
		t.Errorf("Peers(false) = %d, want 2", len(all))
	}

	ready, err := registry.Peers(true)
	if err != nil {
		t.Fatalf("Peers(true): %v", err)
	}
	if len(ready) != 1 || ready[0].Name != "Ready" {
		t.Errorf("Peers(true) = %+v, want Ready only", ready)
	}
}

func TestSignalMailboxDeliversAndDrains(t *testing.T) {
	registry, _ := newTestRegistry(t)

	offer := Signal{
		From:       "alice",
		To:         "bob",
		Kind:       "offer",
		Token:      "aa",
		Candidates: []string{"1.2.3.4:1", "1.2.3.4:1", "", "5.6.7.8:2"},
	}
	if err := registry.PostSignal(offer); err != nil {
		t.Fatalf("PostSignal: %v", err)
	}
	if err := registry.PostSignal(Signal{From: "carol", To: "bob", Kind: "answer"}); err != nil {
		t.Fatalf("PostSignal: %v", err)
	}

	signals := registry.TakeSignals("bob")
	if len(signals) != 2 {
		t.Fatalf("signals = %d, want 2", len(signals))
	}
	if len(signals[0].Candidates) != 2 {
		t.Errorf("candidates were not deduplicated: %v", signals[0].Candidates)
	}
	if signals[0].At.IsZero() {
		t.Error("signal timestamp was not set")
	}
	if len(registry.TakeSignals("bob")) != 0 {
		t.Error("the mailbox should be drained after a take")
	}
}

func TestSignalsExpire(t *testing.T) {
	registry, clk := newTestRegistry(t)

	if err := registry.PostSignal(Signal{From: "alice", To: "bob", Kind: "offer"}); err != nil {
		t.Fatalf("PostSignal: %v", err)
	}
	clk.t = clk.t.Add(SignalTTL + time.Second)

	if got := registry.TakeSignals("bob"); len(got) != 0 {
		t.Errorf("expired signals were returned: %+v", got)
	}
}

func TestPostSignalValidatesInput(t *testing.T) {
	registry, _ := newTestRegistry(t)

	cases := []Signal{
		{To: "bob", Kind: "offer"},
		{From: "alice", Kind: "offer"},
		{From: "alice", To: "alice", Kind: "offer"},
		{From: "alice", To: "bob"},
	}
	for _, signal := range cases {
		if err := registry.PostSignal(signal); err == nil {
			t.Errorf("expected an error for %+v", signal)
		}
	}
}

func TestSignalsAreScopedToRecipient(t *testing.T) {
	registry, _ := newTestRegistry(t)

	if err := registry.PostSignal(Signal{From: "alice", To: "bob", Kind: "offer"}); err != nil {
		t.Fatalf("PostSignal: %v", err)
	}
	if got := registry.TakeSignals("carol"); len(got) != 0 {
		t.Errorf("carol received signals addressed to bob: %+v", got)
	}
	if got := registry.TakeSignals("bob"); len(got) != 1 {
		t.Errorf("bob signals = %d, want 1", len(got))
	}
}
