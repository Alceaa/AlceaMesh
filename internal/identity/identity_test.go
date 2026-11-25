package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

const emptyBodyHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func testKey(t *testing.T) *Key {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for index := range seed {
		seed[index] = 0x11 + byte(index)
	}
	key, err := FromSeed(seed)
	if err != nil {
		t.Fatalf("FromSeed: %v", err)
	}
	return key
}

func TestGenerateProducesUsableKeys(t *testing.T) {
	key, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	message := []byte("hello")
	signature := key.Sign(message)

	public, err := ParsePublicKey(key.PublicKey())
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	if !ed25519.Verify(public, message, signature) {
		t.Fatal("the signature must verify with the public key")
	}
	if ed25519.Verify(public, []byte("hello!"), signature) {
		t.Fatal("a changed message must not verify")
	}

	other, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if other.PublicKey() == key.PublicKey() {
		t.Fatal("two generated keys must differ")
	}
}

func TestBodyHashOfEmptyBody(t *testing.T) {
	if got := BodyHash(nil); got != emptyBodyHash {
		t.Errorf("BodyHash(nil) = %q, want %q", got, emptyBodyHash)
	}
	sum := sha256.Sum256(nil)
	if got := BodyHash([]byte{}); got != hex.EncodeToString(sum[:]) {
		t.Errorf("BodyHash(empty) = %q", got)
	}
}

func TestCanonicalRequestIsExact(t *testing.T) {
	canonical := CanonicalRequest("post", "/v1/presence", "a=1&b=2", 1700000000, "00112233445566778899aabbccddeeff", emptyBodyHash)
	want := strings.Join([]string{
		"POST",
		"/v1/presence",
		"a=1&b=2",
		"1700000000",
		"00112233445566778899aabbccddeeff",
		emptyBodyHash,
	}, "\n")
	if canonical != want {
		t.Errorf("canonical =\n%q\nwant\n%q", canonical, want)
	}
	if strings.Count(canonical, "\n") != 5 {
		t.Errorf("the canonical string must have exactly 5 separators, got %d", strings.Count(canonical, "\n"))
	}
}

func TestSignatureIsDeterministic(t *testing.T) {
	key := testKey(t)
	canonical := CanonicalRequest("GET", "/v1/peers", "", 1700000000, "00112233445566778899aabbccddeeff", emptyBodyHash)

	first := key.Sign([]byte(canonical))
	second := key.Sign([]byte(canonical))
	if base64.StdEncoding.EncodeToString(first) != base64.StdEncoding.EncodeToString(second) {
		t.Error("Ed25519 signatures of the same message must be identical")
	}
	if len(first) != ed25519.SignatureSize {
		t.Errorf("signature length = %d, want %d", len(first), ed25519.SignatureSize)
	}
}

func TestNonceIsRandomAndHex(t *testing.T) {
	first, err := NewNonce()
	if err != nil {
		t.Fatalf("NewNonce: %v", err)
	}
	second, err := NewNonce()
	if err != nil {
		t.Fatalf("NewNonce: %v", err)
	}
	if first == second {
		t.Error("two nonces must differ")
	}
	if len(first) != nonceBytes*2 {
		t.Errorf("nonce length = %d, want %d", len(first), nonceBytes*2)
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Errorf("nonce must be hex: %v", err)
	}
}

func TestParsePublicKeyRejectsWrongLength(t *testing.T) {
	if _, err := ParsePublicKey("not-base64!!"); err == nil {
		t.Error("bad base64 must be rejected")
	}
	short := base64.StdEncoding.EncodeToString([]byte("too short"))
	if _, err := ParsePublicKey(short); err == nil {
		t.Error("a short key must be rejected")
	}
}

func TestFromSeedRejectsWrongLength(t *testing.T) {
	if _, err := FromSeed([]byte("short")); err == nil {
		t.Error("a short seed must be rejected")
	}
}
