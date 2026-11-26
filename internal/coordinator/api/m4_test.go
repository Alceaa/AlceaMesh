package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"alceacore.ru/alceamesh/internal/audit"
	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/identity"
)

type m4Options struct {
	RequireSignature bool
	Limiter          *Limiter
}

func newM4Handler(t *testing.T, options m4Options) (*Server, *coordinator.Registry, *testClock, *bytes.Buffer) {
	t.Helper()

	registry, err := coordinator.NewRegistry(coordinator.NewMemoryStore(), coordinator.Options{
		Pool:        "10.66.66.0/24",
		PresenceTTL: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	buffer := &bytes.Buffer{}
	clock := &testClock{at: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}

	server := &Server{
		Registry:         registry,
		AdminKey:         testAdminKey,
		Version:          "test",
		RequireSignature: options.RequireSignature,
		Limiter:          options.Limiter,
		Audit:            audit.New(buffer),
		Now:              clock.now,
	}
	return server, registry, clock, buffer
}

func newM4Server(t *testing.T, options m4Options) (*httptest.Server, *coordinator.Registry, *testClock, *bytes.Buffer) {
	t.Helper()

	server, registry, clock, buffer := newM4Handler(t, options)
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)
	return httpServer, registry, clock, buffer
}

func enrollAttempt(t *testing.T, handler http.Handler, remote, invite string) (int, map[string]any) {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"invite":          invite,
		"name":            "тестовый",
		"platform":        "windows",
		"public_key":      "wg-key",
		"sign_public_key": "",
		"signature":       "",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, "/v1/enroll", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if remote != "" {
		request.RemoteAddr = remote
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	var payload map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &payload)
	return recorder.Code, payload
}

func TestEnrollRateLimit(t *testing.T) {
	server, _, clock, _ := newM4Handler(t, m4Options{Limiter: NewLimiter(60, 3, 0)})
	handler := server.Handler()
	const remote = "203.0.113.7:4444"

	for attempt := 1; attempt <= 3; attempt++ {
		status, payload := enrollAttempt(t, handler, remote, "ffffffffffffffffffffffffffffffff")
		if status == http.StatusTooManyRequests {
			t.Fatalf("attempt %d must be allowed, payload = %v", attempt, payload)
		}
	}

	status, payload := enrollAttempt(t, handler, remote, "ffffffffffffffffffffffffffffffff")
	if status != http.StatusTooManyRequests {
		t.Fatalf("the fourth attempt must be limited: status = %d, payload = %v", status, payload)
	}
	if payload["reason"] != "rate_limited" {
		t.Errorf("reason = %v", payload["reason"])
	}

	status, _ = enrollAttempt(t, handler, "198.51.100.9:1111", "ffffffffffffffffffffffffffffffff")
	if status == http.StatusTooManyRequests {
		t.Error("another address must not be limited by someone else's attempts")
	}

	clock.advance(10 * time.Second)
	status, _ = enrollAttempt(t, handler, remote, "ffffffffffffffffffffffffffffffff")
	if status == http.StatusTooManyRequests {
		t.Error("after the window the limiter must refill")
	}
}

func TestRateLimitDisabled(t *testing.T) {
	if NewLimiter(0, 0, 0) != nil {
		t.Error("a zero rate must disable the limiter")
	}
	var limiter *Limiter
	allowed, _ := limiter.Allow(time.Now(), "any")
	if !allowed {
		t.Error("a nil limiter must allow everything")
	}
}

func TestAuditRecordsSecurityEvents(t *testing.T) {
	server, registry, _, buffer := newM4Server(t, m4Options{RequireSignature: true})

	status, payload := postJSONAuth(t, server.URL+"/v1/invites", testAdminKey, map[string]any{"label": "Дима", "uses": 1})
	if status != http.StatusCreated {
		t.Fatalf("invite: status = %d, payload = %v", status, payload)
	}
	code, _ := payload["code"].(string)
	invitation, _ := payload["invitation"].(map[string]any)
	inviteID, _ := invitation["id"].(string)

	status, payload = postJSONAuth(t, server.URL+"/v1/invites/"+inviteID+"/revoke", testAdminKey, nil)
	if status != http.StatusOK {
		t.Fatalf("revoke invite: status = %d, payload = %v", status, payload)
	}

	device, err := registry.Announce(coordinator.AnnounceRequest{PublicKey: "wg-audit", Name: "старый"})
	if err != nil {
		t.Fatalf("Announce: %v", err)
	}
	status, payload = postJSONAuth(t, server.URL+"/v1/devices/"+device.ID+"/revoke", testAdminKey, nil)
	if status != http.StatusOK {
		t.Fatalf("revoke device: status = %d, payload = %v", status, payload)
	}

	postJSON(t, server.URL+"/v1/enroll", map[string]any{
		"invite":     code,
		"name":       "тестовый",
		"public_key": "wg-key",
	})

	request, err := http.NewRequest(http.MethodGet, server.URL+"/v1/peers", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	status, _ = send(t, request)
	if status != http.StatusUnauthorized {
		t.Fatalf("an unsigned request must be rejected: status = %d", status)
	}

	entries := decodeAudit(t, buffer)
	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		seen[entry.Event+"/"+entry.Result] = entry.Reason
	}
	for _, want := range []string{"invite/created", "invite/revoked", "device/revoked", "enroll/fail", "auth/deny"} {
		if _, ok := seen[want]; !ok {
			t.Errorf("no audit entry %q in\n%s", want, buffer.String())
		}
	}
	if reason := seen["enroll/fail"]; reason != string(ReasonInviteRevoked) && reason != string(ReasonBadSignKey) {
		t.Errorf("enroll failure reason = %q", reason)
	}

	if strings.Contains(buffer.String(), code) {
		t.Error("an audit entry must never contain the invitation code")
	}
	if strings.Contains(buffer.String(), device.Secret) {
		t.Error("an audit entry must never contain a device secret")
	}
}

func TestAuditRecordsSuccessfulEnrollment(t *testing.T) {
	server, registry, _, buffer := newM4Server(t, m4Options{RequireSignature: true})

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

	var success audit.Entry
	for _, entry := range decodeAudit(t, buffer) {
		if entry.Event == "enroll" && entry.Result == "ok" {
			success = entry
		}
	}
	if success.DeviceID == "" {
		t.Fatalf("no successful enroll entry in\n%s", buffer.String())
	}
	if success.DeviceName != "Димин ноут" {
		t.Errorf("device name = %q", success.DeviceName)
	}
	if success.Remote == "" {
		t.Error("the entry must carry the remote address")
	}
	if strings.Contains(buffer.String(), key.PublicKey()) {
		t.Error("the audit must not store public keys")
	}
}

func decodeAudit(t *testing.T, buffer *bytes.Buffer) []audit.Entry {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	entries := make([]audit.Entry, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry audit.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}
