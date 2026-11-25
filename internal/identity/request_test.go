package identity

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSignAndVerifyRequest(t *testing.T) {
	key := testKey(t)
	now := time.Unix(1700000000, 0)
	body := []byte(`{"deviceName":"Домашний ПК"}`)

	request, err := http.NewRequest(http.MethodPost, "https://mesh.alceacore.ru/v1/presence?x=1", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := key.SignRequest(request, body, "dev-1", now); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	if !HeadersPresent(request.Header) {
		t.Fatal("HeadersPresent must be true after signing")
	}

	signed, err := ParseRequest(request.Header)
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if signed.DeviceID != "dev-1" || signed.Timestamp != now.Unix() {
		t.Errorf("signed = %+v", signed)
	}
	if err := signed.Fresh(now.Add(30*time.Second), DefaultWindow); err != nil {
		t.Errorf("Fresh: %v", err)
	}
	if err := signed.Verify(key.PublicKey(), http.MethodPost, "/v1/presence", "x=1", body); err != nil {
		t.Errorf("Verify: %v", err)
	}

	if err := signed.Verify(key.PublicKey(), http.MethodPost, "/v1/presence", "x=1", []byte("{}")); !errors.Is(err, ErrBadSignature) {
		t.Errorf("a different body must fail: %v", err)
	}
	if err := signed.Verify(key.PublicKey(), http.MethodPost, "/v1/presence", "", body); !errors.Is(err, ErrBadSignature) {
		t.Errorf("a different query must fail: %v", err)
	}
	if err := signed.Verify(key.PublicKey(), http.MethodGet, "/v1/presence", "x=1", body); !errors.Is(err, ErrBadSignature) {
		t.Errorf("a different method must fail: %v", err)
	}

	stranger, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if err := signed.Verify(stranger.PublicKey(), http.MethodPost, "/v1/presence", "x=1", body); !errors.Is(err, ErrBadSignature) {
		t.Errorf("another key must fail: %v", err)
	}
}

func TestSignRequestWithoutBody(t *testing.T) {
	key := testKey(t)
	now := time.Unix(1700000000, 0)

	request, err := http.NewRequest(http.MethodGet, "https://mesh.alceacore.ru/v1/peers", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := key.SignRequest(request, nil, "dev-2", now); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	signed, err := ParseRequest(request.Header)
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if err := signed.Verify(key.PublicKey(), http.MethodGet, "/v1/peers", "", nil); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestFreshWindow(t *testing.T) {
	signed := SignedRequest{Timestamp: 1700000000}
	now := time.Unix(1700000000, 0)

	cases := []struct {
		drift time.Duration
		want  bool
	}{
		{0, true},
		{119 * time.Second, true},
		{-119 * time.Second, true},
		{121 * time.Second, false},
		{-121 * time.Second, false},
	}
	for _, item := range cases {
		err := signed.Fresh(now.Add(item.drift), DefaultWindow)
		if (err == nil) != item.want {
			t.Errorf("drift %s: err = %v, want ok = %v", item.drift, err, item.want)
		}
		if err != nil && !errors.Is(err, ErrStale) {
			t.Errorf("drift %s: err = %v, want ErrStale", item.drift, err)
		}
	}

	if err := signed.Fresh(now, 0); err != nil {
		t.Errorf("a zero window must fall back to the default: %v", err)
	}
}

func TestParseRequestRejectsGarbage(t *testing.T) {
	valid := http.Header{
		HeaderDevice:    {"dev-1"},
		HeaderTimestamp: {"1700000000"},
		HeaderNonce:     {"00112233445566778899aabbccddeeff"},
		HeaderSignature: {base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))},
	}

	broken := map[string]http.Header{
		"no device":    {HeaderTimestamp: valid[HeaderTimestamp], HeaderNonce: valid[HeaderNonce], HeaderSignature: valid[HeaderSignature]},
		"no timestamp": {HeaderDevice: valid[HeaderDevice], HeaderNonce: valid[HeaderNonce], HeaderSignature: valid[HeaderSignature]},
		"bad timestamp": {HeaderDevice: valid[HeaderDevice], HeaderTimestamp: {"вчера"},
			HeaderNonce: valid[HeaderNonce], HeaderSignature: valid[HeaderSignature]},
		"no nonce": {HeaderDevice: valid[HeaderDevice], HeaderTimestamp: valid[HeaderTimestamp], HeaderSignature: valid[HeaderSignature]},
		"nonce not hex": {HeaderDevice: valid[HeaderDevice], HeaderTimestamp: valid[HeaderTimestamp],
			HeaderNonce: {"zzzz"}, HeaderSignature: valid[HeaderSignature]},
		"bad base64": {HeaderDevice: valid[HeaderDevice], HeaderTimestamp: valid[HeaderTimestamp],
			HeaderNonce: valid[HeaderNonce], HeaderSignature: {"***"}},
		"short signature": {HeaderDevice: valid[HeaderDevice], HeaderTimestamp: valid[HeaderTimestamp],
			HeaderNonce: valid[HeaderNonce], HeaderSignature: {base64.StdEncoding.EncodeToString([]byte("short"))}},
	}
	for name, header := range broken {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRequest(header); !errors.Is(err, ErrMalformed) {
				t.Errorf("err = %v, want ErrMalformed", err)
			}
		})
	}

	if !HeadersPresent(valid) {
		t.Error("HeadersPresent must be true for a signed header set")
	}
	if HeadersPresent(http.Header{}) {
		t.Error("HeadersPresent must be false for an empty header set")
	}
}
