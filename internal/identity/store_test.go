package identity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	key, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := key.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, scheme, err := LoadWithScheme(dir)
	if err != nil {
		t.Fatalf("LoadWithScheme: %v", err)
	}
	if loaded.PublicKey() != key.PublicKey() {
		t.Error("the loaded key must match the saved one")
	}
	if scheme != SchemeDPAPI && scheme != SchemePlain {
		t.Errorf("scheme = %q, want %q or %q", scheme, SchemeDPAPI, SchemePlain)
	}

	if _, err := os.Stat(KeyPath(dir)); err != nil {
		t.Errorf("the key file must exist: %v", err)
	}

	raw, err := os.ReadFile(KeyPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !json.Valid(raw) {
		t.Error("the key file must be valid JSON")
	}
	if scheme == SchemeDPAPI && strings.Contains(string(raw), string(key.Seed())) {
		t.Error("with DPAPI the raw seed must not be stored in the file")
	}
}

func TestLoadOrCreateKeepsTheSameKey(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	second, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if first.PublicKey() != second.PublicKey() {
		t.Error("the second call must return the stored key, not a new one")
	}
}

func TestLoadRejectsBrokenFiles(t *testing.T) {
	cases := map[string]string{
		"not json":        `{`,
		"wrong version":   `{"version":99,"scheme":"plain","data":"AAAA"}`,
		"unknown scheme":  `{"version":1,"scheme":"magic","data":"AAAA"}`,
		"bad base64":      `{"version":1,"scheme":"plain","data":"***"}`,
		"wrong seed size": `{"version":1,"scheme":"plain","data":"AAAA"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(KeyPath(dir), []byte(body), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}
			if _, err := Load(dir); err == nil {
				t.Errorf("body %q must be rejected", body)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "empty")
	_, err := Load(dir)
	if !os.IsNotExist(err) {
		t.Errorf("err = %v, want a not-exist error", err)
	}
}

func TestSaveCreatesTheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "identity")
	key, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := key.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := Load(dir); err != nil {
		t.Errorf("Load: %v", err)
	}
}
