package transfer

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"time"
)

const alpn = "alceamesh-transfer"

var certEpoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

func SharedSecret(privateKey, peerPublicKey string) ([]byte, error) {
	privateRaw, err := base64.StdEncoding.DecodeString(privateKey)
	if err != nil {
		return nil, fmt.Errorf("transfer: invalid local private key: %w", err)
	}
	publicRaw, err := base64.StdEncoding.DecodeString(peerPublicKey)
	if err != nil {
		return nil, fmt.Errorf("transfer: invalid peer public key: %w", err)
	}

	private, err := ecdh.X25519().NewPrivateKey(privateRaw)
	if err != nil {
		return nil, fmt.Errorf("transfer: invalid local private key: %w", err)
	}
	public, err := ecdh.X25519().NewPublicKey(publicRaw)
	if err != nil {
		return nil, fmt.Errorf("transfer: invalid peer public key: %w", err)
	}
	return private.ECDH(public)
}

func Certificate(secret []byte) (tls.Certificate, error) {
	if len(secret) == 0 {
		return tls.Certificate{}, errors.New("transfer: empty shared secret")
	}

	seed := sha256.Sum256(append([]byte("alceamesh-transfer-v1"), secret...))
	key := ed25519.NewKeyFromSeed(seed[:])

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "alceamesh"},
		NotBefore:             certEpoch,
		NotAfter:              certEpoch.AddDate(100, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, key.Public(), key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("transfer: create certificate: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

func tlsConfig(cert tls.Certificate) *tls.Config {
	expected := cert.Certificate[0]
	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		NextProtos:         []string{alpn},
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 || !bytes.Equal(raw[0], expected) {
				return errors.New("transfer: peer identity does not match the WireGuard key")
			}
			return nil
		},
	}
}
