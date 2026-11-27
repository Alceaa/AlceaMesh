package trust

import (
	"crypto/tls"
	"strings"
	"testing"
	"time"

	"alceacore.ru/alceamesh/internal/pki"
)

func TestPinnedRootVerifies(t *testing.T) {
	if !Pinned() {
		t.Skip("корень ещё не закреплён в этой сборке")
	}
	if err := VerifyPin(); err != nil {
		t.Fatalf("закреплённый отпечаток не сходится с сертификатом: %v", err)
	}

	config, err := TLSConfig()
	if err != nil {
		t.Fatalf("TLSConfig: %v", err)
	}
	if config.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %x, want TLS 1.3", config.MinVersion)
	}
	if config.ServerName != ServerName {
		t.Errorf("ServerName = %q, want %q", config.ServerName, ServerName)
	}
	if config.RootCAs == nil || len(config.RootCAs.Subjects()) == 0 {
		t.Error("в пуле должен быть наш корень")
	}
	if config.InsecureSkipVerify {
		t.Error("проверка сертификата должна оставаться включённой")
	}

	cert, err := RootCertificate()
	if err != nil {
		t.Fatalf("RootCertificate: %v", err)
	}
	if !cert.IsCA {
		t.Error("закреплённый сертификат должен быть центром сертификации")
	}
	if cert.NotAfter.Before(time.Now()) {
		t.Errorf("корень истёк %s", cert.NotAfter)
	}
}

func TestTemplatesAreUnpinned(t *testing.T) {
	if Pinned() {
		t.Skip("the build already has a pinned root")
	}
	if _, err := TLSConfig(); err == nil {
		t.Fatal("an unpinned build must refuse to build a TLS config")
	}
	if err := VerifyPin(); err == nil {
		t.Error("VerifyPin must fail while the root is not pinned")
	}
	if _, err := RootCertificate(); err == nil {
		t.Error("RootCertificate must fail while the root is not pinned")
	}
}

func TestTLSConfigWithAPinnedRoot(t *testing.T) {
	root, rootPEMBytes, _, err := pki.CreateRoot("Test Root", 10)
	if err != nil {
		t.Fatalf("CreateRoot: %v", err)
	}

	config, err := tlsConfig(string(rootPEMBytes), PinOf(root.Cert), "", "", "mesh.alceacore.ru")
	if err != nil {
		t.Fatalf("tlsConfig: %v", err)
	}
	if config.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %x, want TLS 1.3", config.MinVersion)
	}
	if config.ServerName != "mesh.alceacore.ru" {
		t.Errorf("ServerName = %q", config.ServerName)
	}
	if config.RootCAs == nil {
		t.Error("RootCAs must be set: the system store must not be used")
	}
	if config.InsecureSkipVerify {
		t.Error("certificate verification must stay on")
	}
	if len(config.RootCAs.Subjects()) != 1 {
		t.Errorf("the pool must contain exactly our root, got %d", len(config.RootCAs.Subjects()))
	}

	if _, err := tlsConfig(string(rootPEMBytes), "другой-отпечаток", "", "", "mesh.alceacore.ru"); err == nil {
		t.Error("a pin mismatch must be rejected")
	}
	if _, err := tlsConfig(string(rootPEMBytes), "", "", "", "mesh.alceacore.ru"); err == nil {
		t.Error("a missing pin must be rejected")
	}
	if _, err := tlsConfig("не сертификат", "отпечаток", "", "", "mesh.alceacore.ru"); err == nil {
		t.Error("a broken PEM must be rejected")
	}
}

func TestTLSConfigAcceptsTwoRootsDuringRotation(t *testing.T) {
	oldRoot, oldPEM, _, err := pki.CreateRoot("Старый корень", 10)
	if err != nil {
		t.Fatalf("CreateRoot: %v", err)
	}
	newRoot, newPEM, _, err := pki.CreateRoot("Новый корень", 10)
	if err != nil {
		t.Fatalf("CreateRoot: %v", err)
	}

	config, err := tlsConfig(string(newPEM), PinOf(newRoot.Cert), string(oldPEM), PinOf(oldRoot.Cert), ServerName)
	if err != nil {
		t.Fatalf("tlsConfig: %v", err)
	}
	if len(config.RootCAs.Subjects()) != 2 {
		t.Errorf("during rotation the pool must hold both roots, got %d", len(config.RootCAs.Subjects()))
	}

	if _, err := tlsConfig(string(newPEM), PinOf(newRoot.Cert), string(oldPEM), "чужой-отпечаток", ServerName); err == nil {
		t.Error("a wrong legacy pin must be rejected")
	}
}

func TestPinOfIsStable(t *testing.T) {
	root, _, _, err := pki.CreateRoot("Test Root", 10)
	if err != nil {
		t.Fatalf("CreateRoot: %v", err)
	}
	first := PinOf(root.Cert)
	if first != PinOf(root.Cert) {
		t.Error("the pin must be stable")
	}
	if !strings.Contains(RootPinInstructions(), "root.crt") {
		t.Error("the instructions must mention root.crt")
	}
}
