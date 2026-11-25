package pki

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	RootCertFile   = "root.crt"
	RootKeyFile    = "root.key"
	ServerCertFile = "server.crt"
	ServerKeyFile  = "server.key"
)

type Options struct {
	Dir        string
	CommonName string
	Passphrase string
	Years      int
}

func InitCA(options Options) (*Root, error) {
	if options.Dir == "" {
		return nil, errors.New("pki: a directory is required")
	}
	if options.Passphrase == "" {
		return nil, errors.New("pki: the root key must be protected by a passphrase")
	}

	root, certPEM, key, err := CreateRoot(options.CommonName, options.Years)
	if err != nil {
		return nil, err
	}
	keyPEM, err := EncodePrivateKey(key, options.Passphrase)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(options.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("pki: create %s: %w", options.Dir, err)
	}
	if err := writeFile(filepath.Join(options.Dir, RootCertFile), certPEM, 0o644); err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(options.Dir, RootKeyFile), keyPEM, 0o600); err != nil {
		return nil, err
	}
	return root, nil
}

func OpenCA(dir, passphrase string) (*Root, error) {
	certPEM, err := os.ReadFile(filepath.Join(dir, RootCertFile))
	if err != nil {
		return nil, fmt.Errorf("pki: read root certificate: %w", err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, RootKeyFile))
	if err != nil {
		return nil, fmt.Errorf("pki: read root key: %w", err)
	}
	return LoadRoot(certPEM, keyPEM, passphrase)
}

func (r *Root) IssueServerTo(dir string, options ServerOptions) (*Issued, error) {
	issued, err := r.IssueServer(options)
	if err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(dir, ServerCertFile), issued.CertPEM, 0o644); err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(dir, ServerKeyFile), issued.KeyPEM, 0o600); err != nil {
		return nil, err
	}
	return issued, nil
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("pki: write %s: %w", path, err)
	}
	if err := restrict(path, mode); err != nil {
		return err
	}
	return nil
}
