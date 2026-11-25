package trust

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"alceacore.ru/alceamesh/internal/pki"
)

func startTLSServer(t *testing.T) (*httptest.Server, *pki.Root, []byte) {
	t.Helper()

	root, rootPEM, _, err := pki.CreateRoot("Test Root", 10)
	if err != nil {
		t.Fatalf("CreateRoot: %v", err)
	}
	issued, err := root.IssueServer(pki.ServerOptions{DNSNames: []string{"mesh.alceacore.ru"}})
	if err != nil {
		t.Fatalf("IssueServer: %v", err)
	}
	leaf, err := tls.X509KeyPair(issued.CertPEM, issued.KeyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{leaf}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, root, rootPEM
}

func TestClientTrustsOurRoot(t *testing.T) {
	server, root, rootPEM := startTLSServer(t)

	config, err := tlsConfig(string(rootPEM), PinOf(root.Cert), "", "", ServerName)
	if err != nil {
		t.Fatalf("tlsConfig: %v", err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: config}}

	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("the client must accept a certificate from our root: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q", body)
	}
}

func TestClientRejectsAnotherRoot(t *testing.T) {
	server, _, _ := startTLSServer(t)

	stranger, strangerPEM, _, err := pki.CreateRoot("Чужой корень", 10)
	if err != nil {
		t.Fatalf("CreateRoot: %v", err)
	}
	config, err := tlsConfig(string(strangerPEM), PinOf(stranger.Cert), "", "", ServerName)
	if err != nil {
		t.Fatalf("tlsConfig: %v", err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: config}}

	if _, err := client.Get(server.URL); err == nil {
		t.Fatal("a certificate from another root must be rejected")
	}
}

func TestClientRejectsWrongName(t *testing.T) {
	server, root, rootPEM := startTLSServer(t)

	config, err := tlsConfig(string(rootPEM), PinOf(root.Cert), "", "", "другой.домен")
	if err != nil {
		t.Fatalf("tlsConfig: %v", err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: config}}

	if _, err := client.Get(server.URL); err == nil {
		t.Fatal("a certificate for another name must be rejected")
	}
}
