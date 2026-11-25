package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderDevice    = "X-Alcea-Device"
	HeaderTimestamp = "X-Alcea-Ts"
	HeaderNonce     = "X-Alcea-Nonce"
	HeaderSignature = "X-Alcea-Signature"

	DefaultWindow = 120 * time.Second
	nonceBytes    = 16
)

var (
	ErrMalformed    = errors.New("identity: malformed signature headers")
	ErrStale        = errors.New("identity: timestamp outside the allowed window")
	ErrBadSignature = errors.New("identity: signature does not match")
)

type Key struct {
	private ed25519.PrivateKey
}

type SignedRequest struct {
	DeviceID  string
	Timestamp int64
	Nonce     string
	Signature []byte
}

func Generate() (*Key, error) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("identity: generate key: %w", err)
	}
	return &Key{private: private}, nil
}

func FromSeed(seed []byte) (*Key, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("identity: seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	return &Key{private: ed25519.NewKeyFromSeed(seed)}, nil
}

func (k *Key) Seed() []byte {
	return append([]byte(nil), k.private.Seed()...)
}

func (k *Key) PublicKey() string {
	public := k.private.Public().(ed25519.PublicKey)
	return base64.StdEncoding.EncodeToString(public)
}

func (k *Key) Sign(message []byte) []byte {
	return ed25519.Sign(k.private, message)
}

func ParsePublicKey(encoded string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("identity: public key is not base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("identity: public key must be %d bytes, got %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

func BodyHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func CanonicalRequest(method, path, query string, timestamp int64, nonce, bodyHash string) string {
	return strings.Join([]string{
		strings.ToUpper(method),
		path,
		query,
		strconv.FormatInt(timestamp, 10),
		nonce,
		bodyHash,
	}, "\n")
}

func NewNonce() (string, error) {
	buffer := make([]byte, nonceBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("identity: nonce: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func (k *Key) SignRequest(request *http.Request, body []byte, deviceID string, now time.Time) error {
	nonce, err := NewNonce()
	if err != nil {
		return err
	}
	timestamp := now.Unix()
	canonical := CanonicalRequest(request.Method, request.URL.Path, request.URL.RawQuery, timestamp, nonce, BodyHash(body))

	request.Header.Set(HeaderDevice, deviceID)
	request.Header.Set(HeaderTimestamp, strconv.FormatInt(timestamp, 10))
	request.Header.Set(HeaderNonce, nonce)
	request.Header.Set(HeaderSignature, base64.StdEncoding.EncodeToString(k.Sign([]byte(canonical))))
	return nil
}

func ParseRequest(header http.Header) (SignedRequest, error) {
	deviceID := strings.TrimSpace(header.Get(HeaderDevice))
	if deviceID == "" {
		return SignedRequest{}, fmt.Errorf("%w: no %s", ErrMalformed, HeaderDevice)
	}

	rawTimestamp := strings.TrimSpace(header.Get(HeaderTimestamp))
	timestamp, err := strconv.ParseInt(rawTimestamp, 10, 64)
	if err != nil {
		return SignedRequest{}, fmt.Errorf("%w: bad %s", ErrMalformed, HeaderTimestamp)
	}

	nonce := strings.TrimSpace(header.Get(HeaderNonce))
	if _, err := hex.DecodeString(nonce); err != nil || nonce == "" {
		return SignedRequest{}, fmt.Errorf("%w: bad %s", ErrMalformed, HeaderNonce)
	}

	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(header.Get(HeaderSignature)))
	if err != nil {
		return SignedRequest{}, fmt.Errorf("%w: signature is not base64", ErrMalformed)
	}
	if len(signature) != ed25519.SignatureSize {
		return SignedRequest{}, fmt.Errorf("%w: signature must be %d bytes", ErrMalformed, ed25519.SignatureSize)
	}

	return SignedRequest{DeviceID: deviceID, Timestamp: timestamp, Nonce: nonce, Signature: signature}, nil
}

func (s SignedRequest) Fresh(now time.Time, window time.Duration) error {
	if window <= 0 {
		window = DefaultWindow
	}
	drift := now.Sub(time.Unix(s.Timestamp, 0))
	if drift < 0 {
		drift = -drift
	}
	if drift > window {
		return fmt.Errorf("%w: %s", ErrStale, drift.Round(time.Second))
	}
	return nil
}

func (s SignedRequest) Verify(publicKey string, method, path, query string, body []byte) error {
	key, err := ParsePublicKey(publicKey)
	if err != nil {
		return err
	}
	canonical := CanonicalRequest(method, path, query, s.Timestamp, s.Nonce, BodyHash(body))
	if !ed25519.Verify(key, []byte(canonical), s.Signature) {
		return ErrBadSignature
	}
	return nil
}

func SignMessage(key *Key, message []byte) string {
	return base64.StdEncoding.EncodeToString(key.Sign(message))
}

func VerifySignature(signPublicKey string, message []byte, signature string) error {
	key, err := ParsePublicKey(signPublicKey)
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return fmt.Errorf("identity: signature is not base64: %w", err)
	}
	if len(raw) != ed25519.SignatureSize {
		return fmt.Errorf("identity: signature must be %d bytes", ed25519.SignatureSize)
	}
	if !ed25519.Verify(key, message, raw) {
		return ErrBadSignature
	}
	return nil
}

func CanonicalClaim(claimID, proof, signPublicKey string) []byte {
	return []byte(strings.Join([]string{"alcea-claim-v1", claimID, proof, signPublicKey}, "\n"))
}

func CanonicalEnroll(invite, deviceName, platform, wgPublicKey, signPublicKey string) []byte {
	return []byte(strings.Join([]string{"alcea-enroll-v1", invite, deviceName, platform, wgPublicKey, signPublicKey}, "\n"))
}

func HeadersPresent(header http.Header) bool {
	for _, name := range []string{HeaderDevice, HeaderTimestamp, HeaderNonce, HeaderSignature} {
		if strings.TrimSpace(header.Get(name)) != "" {
			return true
		}
	}
	return false
}
