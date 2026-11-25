package identity

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	keyFileName = "device.key"
	SchemeDPAPI = "dpapi"
	SchemePlain = "plain"
	keyFileVer  = 1
)

type keyFile struct {
	Version int    `json:"version"`
	Scheme  string `json:"scheme"`
	Data    string `json:"data"`
}

func LoadOrCreate(dir string) (*Key, error) {
	key, _, err := LoadWithScheme(dir)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	key, err = Generate()
	if err != nil {
		return nil, err
	}
	if err := key.Save(dir); err != nil {
		return nil, err
	}
	return key, nil
}

func Load(dir string) (*Key, error) {
	key, _, err := LoadWithScheme(dir)
	return key, err
}

func LoadWithScheme(dir string) (*Key, string, error) {
	path := filepath.Join(dir, keyFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}

	var stored keyFile
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, "", fmt.Errorf("identity: %s не читается: %w", keyFileName, err)
	}
	if stored.Version != keyFileVer {
		return nil, "", fmt.Errorf("identity: неподдерживаемая версия файла ключа: %d", stored.Version)
	}
	if stored.Scheme != SchemeDPAPI && stored.Scheme != SchemePlain {
		return nil, "", fmt.Errorf("identity: неизвестная схема защиты ключа: %q", stored.Scheme)
	}

	blob, err := base64.StdEncoding.DecodeString(stored.Data)
	if err != nil {
		return nil, "", fmt.Errorf("identity: данные ключа не в base64: %w", err)
	}
	if stored.Scheme == SchemeDPAPI {
		blob, err = unprotect(blob)
		if err != nil {
			return nil, "", err
		}
	}

	key, err := FromSeed(blob)
	if err != nil {
		return nil, "", err
	}
	return key, stored.Scheme, nil
}

func (k *Key) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	blob, scheme, err := protect(k.Seed())
	if err != nil {
		return err
	}

	stored := keyFile{
		Version: keyFileVer,
		Scheme:  scheme,
		Data:    base64.StdEncoding.EncodeToString(blob),
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(dir, keyFileName)
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return restrict(path)
}

func KeyPath(dir string) string {
	return filepath.Join(dir, keyFileName)
}
