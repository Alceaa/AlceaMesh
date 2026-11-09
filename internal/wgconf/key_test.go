package wgconf

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestPublicKeyMatchesRFC7748Vector(t *testing.T) {
	private, _ := hex.DecodeString("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	want, _ := hex.DecodeString("8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a")

	cfg := &Config{}
	cfg.Interface.PrivateKey = base64.StdEncoding.EncodeToString(private)

	got, err := cfg.PublicKey()
	if err != nil {
		t.Fatalf("PublicKey: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	if !bytes.Equal(raw, want) {
		t.Errorf("public key = %x, want %x", raw, want)
	}
}

func TestPublicKeyRejectsBadInput(t *testing.T) {
	cfg := &Config{}
	cfg.Interface.PrivateKey = "not base64!"
	if _, err := cfg.PublicKey(); err == nil {
		t.Fatal("expected an error for an invalid private key")
	}

	cfg.Interface.PrivateKey = base64.StdEncoding.EncodeToString([]byte("short"))
	if _, err := cfg.PublicKey(); err == nil {
		t.Fatal("expected an error for a short private key")
	}
}

func TestPrimaryAddress(t *testing.T) {
	cfg := &Config{}
	if got := cfg.PrimaryAddress(); got != "" {
		t.Errorf("PrimaryAddress() = %q, want empty", got)
	}

	cfg.Interface.Address = []string{"100.64.0.2/24", "fd00::2/64"}
	if got := cfg.PrimaryAddress(); got != "100.64.0.2" {
		t.Errorf("PrimaryAddress() = %q, want 100.64.0.2", got)
	}

	cfg.Interface.Address = []string{"10.0.0.5"}
	if got := cfg.PrimaryAddress(); got != "10.0.0.5" {
		t.Errorf("PrimaryAddress() = %q, want 10.0.0.5", got)
	}
}
