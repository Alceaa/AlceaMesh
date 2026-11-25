package pki

import (
	"bytes"
	"crypto/x509"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

const testPassphrase = "короткий-тестовый-пароль"

func TestCreateRootAndLoad(t *testing.T) {
	root, certPEM, key, err := CreateRoot("AlceaMesh Test Root", 10)
	if err != nil {
		t.Fatalf("CreateRoot: %v", err)
	}
	if !root.Cert.IsCA || root.Cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Errorf("the root must be a CA able to sign certificates: %+v", root.Cert.KeyUsage)
	}
	if root.Cert.NotAfter.Before(time.Now().AddDate(9, 0, 0)) {
		t.Errorf("the root must live for about ten years, got %s", root.Cert.NotAfter)
	}

	keyPEM, err := EncodePrivateKey(key, testPassphrase)
	if err != nil {
		t.Fatalf("EncodePrivateKey: %v", err)
	}
	if !strings.Contains(string(keyPEM), envelopeType) {
		t.Errorf("an encrypted key must use the %q block", envelopeType)
	}
	if bytes.Contains(keyPEM, key.Seed()) {
		t.Error("the encrypted file must not contain the raw key")
	}

	loaded, err := LoadRoot(certPEM, keyPEM, testPassphrase)
	if err != nil {
		t.Fatalf("LoadRoot: %v", err)
	}
	if !loaded.Cert.Equal(root.Cert) {
		t.Error("the loaded root must match")
	}

	if _, err := LoadRoot(certPEM, keyPEM, "другой-пароль"); !errors.Is(err, ErrWrongPassphrase) {
		t.Errorf("a wrong passphrase: err = %v, want ErrWrongPassphrase", err)
	}
	if _, err := LoadRoot(certPEM, keyPEM, ""); !errors.Is(err, ErrPassphraseRequired) {
		t.Errorf("no passphrase: err = %v, want ErrPassphraseRequired", err)
	}
}

func TestLeafCertificateValidatesAgainstTheRoot(t *testing.T) {
	root, rootPEM, _, err := CreateRoot("AlceaMesh Test Root", 10)
	if err != nil {
		t.Fatalf("CreateRoot: %v", err)
	}

	issued, err := root.IssueServer(ServerOptions{
		DNSNames: []string{"mesh.alceacore.ru"},
		IPs:      []net.IP{net.ParseIP("10.66.66.1")},
		Years:    1,
	})
	if err != nil {
		t.Fatalf("IssueServer: %v", err)
	}
	if !strings.Contains(string(issued.KeyPEM), plainKeyType) {
		t.Error("a server key must be a plain PEM so the service can read it")
	}

	leaf, err := ValidateServer(issued.CertPEM, VerifyOptions{Root: rootPEM, ServerName: "mesh.alceacore.ru"})
	if err != nil {
		t.Fatalf("ValidateServer: %v", err)
	}
	if len(leaf.DNSNames) != 1 || leaf.DNSNames[0] != "mesh.alceacore.ru" {
		t.Errorf("DNS names = %v", leaf.DNSNames)
	}
	if len(leaf.IPAddresses) != 1 || leaf.IPAddresses[0].String() != "10.66.66.1" {
		t.Errorf("IP addresses = %v", leaf.IPAddresses)
	}
	if len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Errorf("extended key usage = %v", leaf.ExtKeyUsage)
	}

	if _, err := ValidateServer(issued.CertPEM, VerifyOptions{Root: rootPEM, ServerName: "другой.домен"}); err == nil {
		t.Error("a certificate must not validate for another name")
	}
	if _, err := ValidateServer(issued.CertPEM, VerifyOptions{
		Root:       rootPEM,
		ServerName: "mesh.alceacore.ru",
		At:         time.Now().AddDate(2, 0, 0),
	}); err == nil {
		t.Error("an expired certificate must not validate")
	}

	other, otherPEM, _, err := CreateRoot("Чужой корень", 10)
	if err != nil {
		t.Fatalf("CreateRoot: %v", err)
	}
	_ = other
	if _, err := ValidateServer(issued.CertPEM, VerifyOptions{Root: otherPEM, ServerName: "mesh.alceacore.ru"}); err == nil {
		t.Error("a certificate from another root must not validate")
	}
}

func TestIssueServerNeedsSANs(t *testing.T) {
	root, _, _, err := CreateRoot("Root", 10)
	if err != nil {
		t.Fatalf("CreateRoot: %v", err)
	}
	if _, err := root.IssueServer(ServerOptions{}); err == nil {
		t.Error("a certificate without SANs must be rejected")
	}
}
