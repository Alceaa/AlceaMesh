package pki

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestInitCAWritesProtectedFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")

	root, err := InitCA(Options{Dir: dir, CommonName: "AlceaMesh Root", Passphrase: testPassphrase, Years: 10})
	if err != nil {
		t.Fatalf("InitCA: %v", err)
	}
	if root == nil || root.Cert == nil {
		t.Fatal("InitCA must return the root")
	}

	for _, name := range []string{RootCertFile, RootKeyFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s must exist: %v", name, err)
		}
	}

	keyPEM, err := os.ReadFile(filepath.Join(dir, RootKeyFile))
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if !bytes.Contains(keyPEM, []byte(envelopeType)) {
		t.Error("the root key must be stored in an encrypted envelope")
	}
	if bytes.Contains(keyPEM, root.Signer.Seed()) {
		t.Error("the root key file must not contain the raw key")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, RootKeyFile))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("the root key mode = %o, want 600", info.Mode().Perm())
		}
	}

	loaded, err := OpenCA(dir, testPassphrase)
	if err != nil {
		t.Fatalf("OpenCA: %v", err)
	}
	if !loaded.Cert.Equal(root.Cert) {
		t.Error("the loaded root must match the created one")
	}
	if _, err := OpenCA(dir, "неверный"); err == nil {
		t.Error("a wrong passphrase must fail")
	}
}

func TestInitCARequiresAPassphrase(t *testing.T) {
	if _, err := InitCA(Options{Dir: t.TempDir(), Passphrase: ""}); err == nil {
		t.Error("a root key without a passphrase must be rejected")
	}
	if _, err := InitCA(Options{Dir: "", Passphrase: "x"}); err == nil {
		t.Error("a missing directory must be rejected")
	}
}

func TestOpenCAMissingFiles(t *testing.T) {
	if _, err := OpenCA(t.TempDir(), "любой"); err == nil {
		t.Error("an empty directory must be rejected")
	}
}

func TestIssueServerToDiskValidates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ca")
	root, err := InitCA(Options{Dir: dir, Passphrase: testPassphrase})
	if err != nil {
		t.Fatalf("InitCA: %v", err)
	}

	issued, err := root.IssueServerTo(dir, ServerOptions{
		DNSNames: []string{"mesh.alceacore.ru"},
		IPs:      []net.IP{net.ParseIP("10.66.66.1")},
	})
	if err != nil {
		t.Fatalf("IssueServerTo: %v", err)
	}

	certPath := filepath.Join(dir, ServerCertFile)
	if _, err := os.Stat(certPath); err != nil {
		t.Fatalf("server.crt must exist: %v", err)
	}
	keyPath := filepath.Join(dir, ServerKeyFile)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(keyPath)
		if err != nil {
			t.Fatalf("stat server key: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("the server key mode = %o, want 600", info.Mode().Perm())
		}
	}

	rootPEM, err := os.ReadFile(filepath.Join(dir, RootCertFile))
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	if _, err := ValidateServer(issued.CertPEM, VerifyOptions{Root: rootPEM, ServerName: "mesh.alceacore.ru"}); err != nil {
		t.Errorf("the issued certificate must validate: %v", err)
	}

	pin, err := Pin(issued.Certificate)
	if err != nil {
		t.Fatalf("Pin: %v", err)
	}
	rootPin, err := Pin(root.Cert)
	if err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if pin == rootPin {
		t.Error("the leaf pin must differ from the root pin")
	}
	if len(pin) == 0 || len(rootPin) == 0 {
		t.Error("pins must not be empty")
	}
}
