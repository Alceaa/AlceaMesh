package identity

import (
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const (
	ChallengeBytes = 32
	dhKeyBytes     = 32
)

func NewEphemeralKey() (*ecdh.PrivateKey, error) {
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("identity: generate x25519 key: %w", err)
	}
	return private, nil
}

func X25519PublicKey(private *ecdh.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())
}

func ParseX25519PublicKey(encoded string) (*ecdh.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("identity: x25519 key is not base64: %w", err)
	}
	if len(raw) != dhKeyBytes {
		return nil, fmt.Errorf("identity: x25519 key must be %d bytes, got %d", dhKeyBytes, len(raw))
	}
	public, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("identity: x25519 key is invalid: %w", err)
	}
	return public, nil
}

func X25519PrivateKeyFromRaw(raw []byte) (*ecdh.PrivateKey, error) {
	if len(raw) != dhKeyBytes {
		return nil, fmt.Errorf("identity: x25519 secret must be %d bytes, got %d", dhKeyBytes, len(raw))
	}
	private, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("identity: x25519 secret is invalid: %w", err)
	}
	return private, nil
}

func SharedSecret(private *ecdh.PrivateKey, peerPublicKey string) ([]byte, error) {
	public, err := ParseX25519PublicKey(peerPublicKey)
	if err != nil {
		return nil, err
	}
	secret, err := private.ECDH(public)
	if err != nil {
		return nil, fmt.Errorf("identity: x25519 agreement failed: %w", err)
	}
	return secret, nil
}

func NewChallenge() ([]byte, error) {
	challenge := make([]byte, ChallengeBytes)
	if _, err := rand.Read(challenge); err != nil {
		return nil, fmt.Errorf("identity: challenge: %w", err)
	}
	return challenge, nil
}

func Proof(sharedSecret, challenge []byte) []byte {
	mac := hmac.New(sha256.New, sharedSecret)
	mac.Write([]byte("alceamesh-claim-v1"))
	mac.Write(challenge)
	return mac.Sum(nil)
}

func ProofMatches(expected, got []byte) bool {
	return hmac.Equal(expected, got)
}

func EncodeToken(value []byte) string {
	return base64.StdEncoding.EncodeToString(value)
}

func DecodeToken(encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("identity: value is not base64: %w", err)
	}
	return raw, nil
}
