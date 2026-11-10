package transfer

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func randomSecret(t *testing.T) []byte {
	t.Helper()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return secret
}

func loopbackPair(t *testing.T) (net.PacketConn, net.PacketConn, net.Addr, net.Addr) {
	t.Helper()

	left, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen left: %v", err)
	}
	right, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		left.Close()
		t.Fatalf("listen right: %v", err)
	}

	t.Cleanup(func() {
		left.Close()
		right.Close()
	})
	return left, right, left.LocalAddr(), right.LocalAddr()
}

func writeFile(t *testing.T, path string, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		t.Fatalf("rand: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return data
}

func TestSendAndReceiveFile(t *testing.T) {
	secret := randomSecret(t)
	cert, err := Certificate(secret)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}

	sender, receiver, senderAddr, receiverAddr := loopbackPair(t)
	_ = senderAddr

	dir := t.TempDir()
	source := filepath.Join(dir, "payload.bin")
	want := writeFile(t, source, 512*1024)

	inbox := filepath.Join(dir, "inbox")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	type outcome struct {
		progress Progress
		err      error
	}
	received := make(chan outcome, 1)
	go func() {
		progress, err := Receive(ctx, receiver, cert, inbox, nil)
		received <- outcome{progress, err}
	}()

	progress, sendErr := Send(ctx, sender, cert, receiverAddr, source, nil)
	result := <-received
	if sendErr != nil {
		t.Fatalf("Send: %v (receiver said: %v)", sendErr, result.err)
	}
	if result.err != nil {
		t.Fatalf("Receive: %v", result.err)
	}
	if progress.Sent != int64(len(want)) {
		t.Errorf("sent = %d, want %d", progress.Sent, len(want))
	}

	got, err := os.ReadFile(result.progress.Path)
	if err != nil {
		t.Fatalf("read received file: %v", err)
	}
	gotSum := sha256.Sum256(got)
	wantSum := sha256.Sum256(want)
	if hex.EncodeToString(gotSum[:]) != hex.EncodeToString(wantSum[:]) {
		t.Error("received file differs from the source")
	}
	if result.progress.Resumed {
		t.Error("fresh transfer should not be marked as resumed")
	}
}

func TestReceiveResumesPartialFile(t *testing.T) {
	secret := randomSecret(t)
	cert, err := Certificate(secret)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}

	sender, receiver, _, receiverAddr := loopbackPair(t)

	dir := t.TempDir()
	source := filepath.Join(dir, "big.bin")
	want := writeFile(t, source, 256*1024)

	inbox := filepath.Join(dir, "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	partial := filepath.Join(inbox, "big.bin"+partSuffix)
	if err := os.WriteFile(partial, want[:100*1024], 0o644); err != nil {
		t.Fatalf("write partial: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	type outcome struct {
		progress Progress
		err      error
	}
	received := make(chan outcome, 1)
	go func() {
		progress, err := Receive(ctx, receiver, cert, inbox, nil)
		received <- outcome{progress, err}
	}()

	progress, sendErr := Send(ctx, sender, cert, receiverAddr, source, nil)
	result := <-received
	if sendErr != nil {
		t.Fatalf("Send: %v (receiver said: %v)", sendErr, result.err)
	}
	if result.err != nil {
		t.Fatalf("Receive: %v", result.err)
	}
	if !progress.Resumed {
		t.Error("sender should report a resumed transfer")
	}
	if progress.Sent != int64(len(want)) {
		t.Errorf("sent = %d, want %d", progress.Sent, len(want))
	}
	if !result.progress.Resumed {
		t.Error("receiver should report a resumed transfer")
	}

	got, err := os.ReadFile(result.progress.Path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if sha256.Sum256(got) != sha256.Sum256(want) {
		t.Error("resumed file differs from the source")
	}
}

func TestCertificateIsDeterministic(t *testing.T) {
	secret := randomSecret(t)

	first, err := Certificate(secret)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	second, err := Certificate(secret)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	if string(first.Certificate[0]) != string(second.Certificate[0]) {
		t.Error("certificates derived from the same secret must be identical")
	}

	other, err := Certificate(randomSecret(t))
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	if string(first.Certificate[0]) == string(other.Certificate[0]) {
		t.Error("certificates from different secrets must differ")
	}
}

func TestSharedSecretIsSymmetric(t *testing.T) {
	alice, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate alice: %v", err)
	}
	bob, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate bob: %v", err)
	}

	aliceView, err := SharedSecret(
		base64.StdEncoding.EncodeToString(alice.Bytes()),
		base64.StdEncoding.EncodeToString(bob.PublicKey().Bytes()),
	)
	if err != nil {
		t.Fatalf("alice shared secret: %v", err)
	}
	bobView, err := SharedSecret(
		base64.StdEncoding.EncodeToString(bob.Bytes()),
		base64.StdEncoding.EncodeToString(alice.PublicKey().Bytes()),
	)
	if err != nil {
		t.Fatalf("bob shared secret: %v", err)
	}
	if string(aliceView) != string(bobView) {
		t.Error("both sides must derive the same shared secret")
	}
	if _, err := SharedSecret("not-base64", "also-bad"); err == nil {
		t.Error("expected an error for invalid keys")
	}
}
