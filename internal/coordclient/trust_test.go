package coordclient

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"alceacore.ru/alceamesh/internal/identity"
	"alceacore.ru/alceamesh/internal/pki"
)

const testServerName = "mesh.alceacore.ru"

type tlsServer struct {
	server  *httptest.Server
	rootPEM []byte
	hits    int32
	last    atomic.Value
}

func startServer(t *testing.T, handler http.HandlerFunc) *tlsServer {
	t.Helper()

	root, rootPEM, _, err := pki.CreateRoot("Test Root", 10)
	if err != nil {
		t.Fatalf("CreateRoot: %v", err)
	}
	issued, err := root.IssueServer(pki.ServerOptions{DNSNames: []string{testServerName}})
	if err != nil {
		t.Fatalf("IssueServer: %v", err)
	}
	leaf, err := tls.X509KeyPair(issued.CertPEM, issued.KeyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}

	wrapper := &tlsServer{rootPEM: rootPEM}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&wrapper.hits, 1)
		wrapper.last.Store(r.Clone(r.Context()))
		handler(w, r)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{leaf}}
	server.StartTLS()
	t.Cleanup(server.Close)
	wrapper.server = server
	return wrapper
}

func poolFrom(rootPEM []byte) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(rootPEM)
	return pool
}

func endpointFor(name string, server *httptest.Server) Endpoint {
	return Endpoint{
		Name:       name,
		BaseURL:    server.URL,
		ServerName: testServerName,
	}
}

func pongServer(t *testing.T) *tlsServer {
	t.Helper()
	return startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"pong":true}`))
	})
}

func TestClientPrefersTheMeshEndpoint(t *testing.T) {
	mesh := pongServer(t)
	public := pongServer(t)

	client, err := NewClient(Options{
		Endpoints: []Endpoint{endpointFor("mesh", mesh.server), endpointFor("public", public.server)},
		TLS:       &tls.Config{RootCAs: poolFrom(mesh.rootPEM), MinVersion: tls.VersionTLS13},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if mesh.hits != 1 {
		t.Errorf("mesh hits = %d, want 1", mesh.hits)
	}
	if public.hits != 0 {
		t.Errorf("the public endpoint must not be used while the mesh one works, hits = %d", public.hits)
	}
	if client.ActiveEndpoint().Name != "mesh" {
		t.Errorf("active endpoint = %q", client.ActiveEndpoint().Name)
	}
}

func TestClientFallsBackToThePublicEndpoint(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := dead.URL
	dead.Close()

	public := pongServer(t)

	client, err := NewClient(Options{
		Endpoints: []Endpoint{
			{Name: "mesh", BaseURL: address, ServerName: testServerName},
			endpointFor("public", public.server),
		},
		TLS: &tls.Config{RootCAs: poolFrom(public.rootPEM), MinVersion: tls.VersionTLS13},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Ping(); err != nil {
		t.Fatalf("Ping must succeed through the fallback: %v", err)
	}
	if public.hits != 1 {
		t.Errorf("public hits = %d, want 1", public.hits)
	}
	if client.ActiveEndpoint().Name != "public" {
		t.Errorf("active endpoint = %q, want public", client.ActiveEndpoint().Name)
	}

	if err := client.Ping(); err != nil {
		t.Fatalf("second Ping: %v", err)
	}
	if public.hits != 2 {
		t.Errorf("the working endpoint must be remembered: hits = %d, want 2", public.hits)
	}
}

func TestClientRefusesAForeignCertificate(t *testing.T) {
	impostor := pongServer(t)
	_, ourRootPEM, _, err := pki.CreateRoot("Наш корень", 10)
	if err != nil {
		t.Fatalf("CreateRoot: %v", err)
	}

	client, err := NewClient(Options{
		Endpoints: []Endpoint{endpointFor("mesh", impostor.server)},
		TLS:       &tls.Config{RootCAs: poolFrom(ourRootPEM), MinVersion: tls.VersionTLS13},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	pingErr := client.Ping()
	if pingErr == nil {
		t.Fatal("a coordinator with a foreign certificate must be refused")
	}
	if !strings.Contains(pingErr.Error(), "certificate") {
		t.Logf("ошибка отказа: %v", pingErr)
	}
}

func TestClientDoesNotFallBackOnRejection(t *testing.T) {
	strict := startServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"device was revoked","reason":"revoked"}`))
	})
	public := pongServer(t)

	client, err := NewClient(Options{
		Endpoints: []Endpoint{endpointFor("mesh", strict.server), endpointFor("public", public.server)},
		TLS:       &tls.Config{RootCAs: poolFrom(strict.rootPEM), MinVersion: tls.VersionTLS13},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	err = client.Ping()
	var rejection *identity.RejectedError
	if !errors.As(err, &rejection) {
		t.Fatalf("err = %v, want a typed rejection", err)
	}
	if !rejection.Matches(identity.ReasonRevoked) {
		t.Errorf("reason = %q, want revoked", rejection.Reason)
	}
	if public.hits != 0 {
		t.Errorf("the client must not fall back when the server answered: public hits = %d", public.hits)
	}
}

func TestClientSignsRequests(t *testing.T) {
	var verified int32
	key, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}
	const deviceID = "dev-test"

	server := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		signed, err := identity.ParseRequest(r.Header)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"reason":"malformed"}`))
			return
		}
		if signed.DeviceID != deviceID {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"reason":"unknown_device"}`))
			return
		}
		if err := signed.Fresh(time.Now(), identity.DefaultWindow); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"reason":"stale"}`))
			return
		}
		if err := signed.Verify(key.PublicKey(), r.Method, r.URL.Path, r.URL.RawQuery, nil); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"reason":"bad_signature"}`))
			return
		}
		atomic.AddInt32(&verified, 1)
		_, _ = w.Write([]byte(`{"pong":true}`))
	})

	client, err := NewClient(Options{
		Endpoints: []Endpoint{endpointFor("mesh", server.server)},
		TLS:       &tls.Config{RootCAs: poolFrom(server.rootPEM), MinVersion: tls.VersionTLS13},
		Signer:    identity.Credentials{DeviceID: deviceID, Key: key},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err := client.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if err := client.Ping(); err != nil {
		t.Fatalf("second Ping: %v", err)
	}
	if verified != 2 {
		t.Errorf("the server verified %d signed requests, want 2", verified)
	}

	stored, ok := server.last.Load().(*http.Request)
	if !ok || stored == nil {
		t.Fatal("no request recorded")
	}
	if stored.Header.Get(identity.HeaderNonce) == "" {
		t.Error("the signature headers must be present")
	}
}

func TestClientWithoutSignerSendsNoSignature(t *testing.T) {
	server := pongServer(t)
	client, err := NewClient(Options{
		Endpoints: []Endpoint{endpointFor("mesh", server.server)},
		TLS:       &tls.Config{RootCAs: poolFrom(server.rootPEM), MinVersion: tls.VersionTLS13},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	stored, ok := server.last.Load().(*http.Request)
	if !ok || stored == nil {
		t.Fatal("no request recorded")
	}
	if stored.Header.Get(identity.HeaderSignature) != "" {
		t.Error("without a signer the request must stay unsigned (legacy mode)")
	}
}

func TestEveryAttemptGetsAFreshSignature(t *testing.T) {
	key, err := identity.Generate()
	if err != nil {
		t.Fatalf("identity.Generate: %v", err)
	}
	failing := startServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	})

	client, err := NewClient(Options{
		Endpoints: []Endpoint{endpointFor("mesh", failing.server)},
		TLS:       &tls.Config{RootCAs: poolFrom(failing.rootPEM), MinVersion: tls.VersionTLS13},
		Signer:    identity.Credentials{DeviceID: "dev", Key: key},
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Ping(); err == nil {
		t.Fatal("a 500 must be reported")
	}

	if err := client.Ping(); err == nil {
		t.Fatal("a 500 must be reported again")
	}
	if failing.hits != 2 {
		t.Errorf("hits = %d, want 2", failing.hits)
	}
	if stored, ok := failing.last.Load().(*http.Request); !ok || stored == nil {
		t.Fatal("no request recorded")
	} else if stored.Header.Get(identity.HeaderNonce) == "" {
		t.Error("each attempt must be signed with a fresh nonce")
	}
}

func TestRejectionParsing(t *testing.T) {
	payload, err := json.Marshal(map[string]string{"error": "device was revoked", "reason": "revoked"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rejection := identity.ParseRejection(http.StatusUnauthorized, payload)
	if rejection == nil {
		t.Fatal("a rejection must be parsed")
	}
	if !rejection.Matches(identity.ReasonRevoked) {
		t.Errorf("reason = %q", rejection.Reason)
	}
	if rejection.Status != http.StatusUnauthorized {
		t.Errorf("status = %d", rejection.Status)
	}
	if identity.ParseRejection(http.StatusUnauthorized, []byte("not json")) != nil {
		t.Error("a non-JSON body must not produce a rejection")
	}
	if text := identity.ReasonRevoked.HumanText(); !strings.Contains(text, "отозвано") {
		t.Errorf("human text = %q", text)
	}
}
