package pki

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
)

const (
	certificateType = "CERTIFICATE"
	plainKeyType    = "PRIVATE KEY"
	envelopeType    = "ALCEAMESH PRIVATE KEY"

	envelopeVersion = 1
	envelopeKDF     = "pbkdf2-sha256"
	envelopeRounds  = 600000
	saltBytes       = 16
	keyBytes        = 32
)

var (
	ErrPassphraseRequired = errors.New("pki: a passphrase is required for this key")
	ErrWrongPassphrase    = errors.New("pki: the passphrase does not match")
)

type keyEnvelope struct {
	Version    int    `json:"version"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Data       string `json:"data"`
}

func EncodeCertificate(cert *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: certificateType, Bytes: cert.Raw})
}

func ParseCertificate(encoded []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(encoded)
	if block == nil || block.Type != certificateType {
		return nil, errors.New("pki: no certificate in PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("pki: parse certificate: %w", err)
	}
	return cert, nil
}

func EncodePrivateKey(key ed25519.PrivateKey, passphrase string) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("pki: marshal key: %w", err)
	}
	if passphrase == "" {
		return pem.EncodeToMemory(&pem.Block{Type: plainKeyType, Bytes: der}), nil
	}
	blob, err := seal(der, passphrase)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: envelopeType, Bytes: blob}), nil
}

func DecodePrivateKey(encoded []byte, passphrase string) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(encoded)
	if block == nil {
		return nil, errors.New("pki: no private key in PEM")
	}

	switch block.Type {
	case plainKeyType:
		return parsePKCS8(block.Bytes)
	case envelopeType:
		if passphrase == "" {
			return nil, ErrPassphraseRequired
		}
		der, err := open(block.Bytes, passphrase)
		if err != nil {
			return nil, err
		}
		return parsePKCS8(der)
	default:
		return nil, fmt.Errorf("pki: unexpected PEM block %q", block.Type)
	}
}

func parsePKCS8(der []byte) (ed25519.PrivateKey, error) {
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("pki: parse key: %w", err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("pki: expected an ed25519 key, got %T", parsed)
	}
	return key, nil
}

func seal(plain []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("pki: salt: %w", err)
	}
	key, err := deriveKey(passphrase, salt, envelopeRounds)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("pki: nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonce, plain, salt)

	envelope := keyEnvelope{
		Version:    envelopeVersion,
		KDF:        envelopeKDF,
		Iterations: envelopeRounds,
		Salt:       base64.StdEncoding.EncodeToString(salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Data:       base64.StdEncoding.EncodeToString(sealed),
	}
	return json.Marshal(envelope)
}

func open(encoded []byte, passphrase string) ([]byte, error) {
	var envelope keyEnvelope
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return nil, fmt.Errorf("pki: key envelope is not readable: %w", err)
	}
	if envelope.Version != envelopeVersion || envelope.KDF != envelopeKDF {
		return nil, fmt.Errorf("pki: unsupported key envelope (version %d, kdf %q)", envelope.Version, envelope.KDF)
	}
	salt, err := base64.StdEncoding.DecodeString(envelope.Salt)
	if err != nil {
		return nil, fmt.Errorf("pki: salt is not base64: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return nil, fmt.Errorf("pki: nonce is not base64: %w", err)
	}
	sealed, err := base64.StdEncoding.DecodeString(envelope.Data)
	if err != nil {
		return nil, fmt.Errorf("pki: key data is not base64: %w", err)
	}

	rounds := envelope.Iterations
	if rounds <= 0 {
		rounds = envelopeRounds
	}
	key, err := deriveKey(passphrase, salt, rounds)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, nonce, sealed, salt)
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	return plain, nil
}

func deriveKey(passphrase string, salt []byte, rounds int) ([]byte, error) {
	key, err := pbkdf2.Key(sha256.New, passphrase, salt, rounds, keyBytes)
	if err != nil {
		return nil, fmt.Errorf("pki: derive key: %w", err)
	}
	return key, nil
}
