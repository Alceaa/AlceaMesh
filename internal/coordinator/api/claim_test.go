package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/identity"
)

type testClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func newClaimServer(t *testing.T) (*httptest.Server, *coordinator.Registry, *testClock) {
	t.Helper()

	registry, err := coordinator.NewRegistry(coordinator.NewMemoryStore(), coordinator.Options{
		Pool:        "10.0.0.0/24",
		PresenceTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	clock := &testClock{at: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}

	server := httptest.NewServer((&Server{
		Registry:         registry,
		AdminKey:         testAdminKey,
		Version:          "test",
		RequireSignature: true,
		Now:              clock.now,
	}).Handler())
	t.Cleanup(server.Close)
	return server, registry, clock
}

func postJSON(t *testing.T, url string, payload any) (int, map[string]any) {
	t.Helper()

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	response, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer response.Body.Close()

	var decoded map[string]any
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return response.StatusCode, decoded
}

type claimFixture struct {
	server    *httptest.Server
	registry  *coordinator.Registry
	clock     *testClock
	device    *coordinator.Device
	signKey   *identity.Key
	claimID   string
	proof     string
	challenge []byte
}

func prepareClaim(t *testing.T, server *httptest.Server, registry *coordinator.Registry, clock *testClock) *claimFixture {
	t.Helper()

	wgKey, err := identity.NewEphemeralKey()
	if err != nil {
		t.Fatalf("NewEphemeralKey: %v", err)
	}
	wgPublic := identity.X25519PublicKey(wgKey)
	device, err := registry.Announce(coordinator.AnnounceRequest{PublicKey: wgPublic, Name: "старый ноут"})
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}

	status, payload := postJSON(t, server.URL+"/v1/claim/challenge", map[string]any{"public_key": wgPublic})
	if status != http.StatusOK {
		t.Fatalf("challenge: status = %d, payload = %v", status, payload)
	}

	claimID, _ := payload["claim_id"].(string)
	serverKey, _ := payload["server_key"].(string)
	challengeEncoded, _ := payload["challenge"].(string)
	challenge, err := identity.DecodeToken(challengeEncoded)
	if err != nil {
		t.Fatalf("decode challenge: %v", err)
	}

	shared, err := identity.SharedSecret(wgKey, serverKey)
	if err != nil {
		t.Fatalf("SharedSecret: %v", err)
	}
	signKey, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}

	return &claimFixture{
		server:    server,
		registry:  registry,
		clock:     clock,
		device:    device,
		signKey:   signKey,
		claimID:   claimID,
		proof:     identity.EncodeToken(identity.Proof(shared, challenge)),
		challenge: challenge,
	}
}

func TestClaimAttachesExistingDevice(t *testing.T) {
	server, registry, clock := newClaimServer(t)
	fixture := prepareClaim(t, server, registry, clock)

	status, payload := postJSON(t, server.URL+"/v1/claim", map[string]any{
		"claim_id":        fixture.claimID,
		"proof":           fixture.proof,
		"sign_public_key": fixture.signKey.PublicKey(),
		"signature":       identity.SignMessage(fixture.signKey, identity.CanonicalClaim(fixture.claimID, fixture.proof, fixture.signKey.PublicKey())),
	})
	if status != http.StatusOK {
		t.Fatalf("claim: status = %d, payload = %v", status, payload)
	}
	device, ok := payload["device"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %v", payload)
	}
	if device["sign_public_key"] != fixture.signKey.PublicKey() {
		t.Errorf("sign key = %v, want %v", device["sign_public_key"], fixture.signKey.PublicKey())
	}

	request, err := http.NewRequest(http.MethodGet, server.URL+"/v1/peers", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := fixture.signKey.SignRequest(request, nil, fixture.device.ID, clock.now()); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	status, payload = send(t, request)
	if status != http.StatusOK {
		t.Fatalf("the attached device must pass signature checks: status = %d, payload = %v", status, payload)
	}
}

func claimBody(fixture *claimFixture, proof string) map[string]any {
	return map[string]any{
		"claim_id":        fixture.claimID,
		"proof":           proof,
		"sign_public_key": fixture.signKey.PublicKey(),
		"signature":       identity.SignMessage(fixture.signKey, identity.CanonicalClaim(fixture.claimID, proof, fixture.signKey.PublicKey())),
	}
}

func TestClaimRejectsBadProof(t *testing.T) {
	server, registry, clock := newClaimServer(t)
	fixture := prepareClaim(t, server, registry, clock)

	tampered, err := identity.DecodeToken(fixture.proof)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	tampered[0] ^= 0xFF

	status, payload := postJSON(t, server.URL+"/v1/claim", claimBody(fixture, identity.EncodeToken(tampered)))
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, payload = %v", status, payload)
	}
	if got := reasonOf(t, payload); got != string(ReasonBadProof) {
		t.Errorf("reason = %q, want %q", got, ReasonBadProof)
	}
}

func TestClaimRejectsWrongSignKey(t *testing.T) {
	server, registry, clock := newClaimServer(t)

	other, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}

	cases := map[string]func(fixture *claimFixture) map[string]any{
		"not a key": func(fixture *claimFixture) map[string]any {
			return map[string]any{
				"claim_id":        fixture.claimID,
				"proof":           fixture.proof,
				"sign_public_key": "не-ключ",
				"signature": identity.SignMessage(fixture.signKey,
					identity.CanonicalClaim(fixture.claimID, fixture.proof, "не-ключ")),
			}
		},
		"signature by another key": func(fixture *claimFixture) map[string]any {
			return map[string]any{
				"claim_id":        fixture.claimID,
				"proof":           fixture.proof,
				"sign_public_key": fixture.signKey.PublicKey(),
				"signature": identity.SignMessage(other,
					identity.CanonicalClaim(fixture.claimID, fixture.proof, fixture.signKey.PublicKey())),
			}
		},
		"broken signature": func(fixture *claimFixture) map[string]any {
			return map[string]any{
				"claim_id":        fixture.claimID,
				"proof":           fixture.proof,
				"sign_public_key": fixture.signKey.PublicKey(),
				"signature":       "***",
			}
		},
		"signature over other fields": func(fixture *claimFixture) map[string]any {
			return map[string]any{
				"claim_id":        fixture.claimID,
				"proof":           fixture.proof,
				"sign_public_key": fixture.signKey.PublicKey(),
				"signature": identity.SignMessage(fixture.signKey,
					identity.CanonicalClaim("другой", fixture.proof, fixture.signKey.PublicKey())),
			}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := prepareClaim(t, server, registry, clock)
			status, payload := postJSON(t, server.URL+"/v1/claim", build(fixture))
			if status != http.StatusUnauthorized {
				t.Fatalf("status = %d, payload = %v", status, payload)
			}
			if got := reasonOf(t, payload); got != string(ReasonBadSignKey) {
				t.Errorf("reason = %q, want %q", got, ReasonBadSignKey)
			}
		})
	}
}

func TestClaimIsOneTimeAndExpires(t *testing.T) {
	server, registry, clock := newClaimServer(t)

	first := prepareClaim(t, server, registry, clock)
	status, payload := postJSON(t, server.URL+"/v1/claim", claimBody(first, first.proof))
	if status != http.StatusOK {
		t.Fatalf("first claim: status = %d, payload = %v", status, payload)
	}

	status, payload = postJSON(t, server.URL+"/v1/claim", claimBody(first, first.proof))
	if status != http.StatusUnauthorized || reasonOf(t, payload) != string(ReasonUnknownClaim) {
		t.Errorf("reuse: status = %d, payload = %v", status, payload)
	}

	second := prepareClaim(t, server, registry, clock)
	clock.advance(claimTTL + time.Minute)
	status, payload = postJSON(t, server.URL+"/v1/claim", claimBody(second, second.proof))
	if status != http.StatusUnauthorized || reasonOf(t, payload) != string(ReasonClaimExpired) {
		t.Errorf("expired: status = %d, payload = %v", status, payload)
	}

	status, payload = postJSON(t, server.URL+"/v1/claim", map[string]any{
		"claim_id":        "нет-такого",
		"proof":           second.proof,
		"sign_public_key": second.signKey.PublicKey(),
		"signature":       "***",
	})
	if status != http.StatusUnauthorized || reasonOf(t, payload) != string(ReasonUnknownClaim) {
		t.Errorf("unknown claim: status = %d, payload = %v", status, payload)
	}
}

func TestClaimChallengeRejectsUnusableDevices(t *testing.T) {
	server, registry, clock := newClaimServer(t)

	status, payload := postJSON(t, server.URL+"/v1/claim/challenge", map[string]any{"public_key": "нет-такого"})
	if status != http.StatusUnauthorized || reasonOf(t, payload) != string(ReasonUnknownDevice) {
		t.Errorf("unknown key: status = %d, payload = %v", status, payload)
	}

	revoked, err := registry.Announce(coordinator.AnnounceRequest{PublicKey: "wg-revoked", Name: "списанный"})
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	if _, err := registry.RevokeDevice(revoked.ID); err != nil {
		t.Fatalf("RevokeDevice: %v", err)
	}
	status, payload = postJSON(t, server.URL+"/v1/claim/challenge", map[string]any{"public_key": "wg-revoked"})
	if status != http.StatusUnauthorized || reasonOf(t, payload) != string(ReasonRevoked) {
		t.Errorf("revoked: status = %d, payload = %v", status, payload)
	}

	fixture := prepareClaim(t, server, registry, clock)
	if _, err := registry.AttachDevice(fixture.device.ID, fixture.signKey.PublicKey()); err != nil {
		t.Fatalf("AttachDevice: %v", err)
	}
	status, payload = postJSON(t, server.URL+"/v1/claim/challenge", map[string]any{"public_key": fixture.device.PublicKey})
	if status != http.StatusUnauthorized || reasonOf(t, payload) != string(ReasonNotAttachable) {
		t.Errorf("already attached: status = %d, payload = %v", status, payload)
	}

	status, payload = postJSON(t, server.URL+"/v1/claim", claimBody(fixture, fixture.proof))
	if status != http.StatusUnauthorized || reasonOf(t, payload) != string(ReasonNotAttachable) {
		t.Errorf("claim for an attached device: status = %d, payload = %v", status, payload)
	}
}
