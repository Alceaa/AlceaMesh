package transfer

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
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

func TestSendAndReceiveFolder(t *testing.T) {
	secret := randomSecret(t)
	cert, err := Certificate(secret)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}

	sender, receiver, _, receiverAddr := loopbackPair(t)

	dir := t.TempDir()
	source := filepath.Join(dir, "Docs")
	nested := filepath.Join(source, "sub", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	top := writeFile(t, filepath.Join(source, "a.txt"), 4096)
	writeFile(t, filepath.Join(source, "sub", "b.bin"), 256*1024)
	deep := writeFile(t, filepath.Join(nested, "c.txt"), 1024)

	inbox := filepath.Join(dir, "inbox")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	if progress.Name != "Docs" {
		t.Errorf("batch name = %q, want %q", progress.Name, "Docs")
	}
	if progress.Files != 3 {
		t.Errorf("files = %d, want 3", progress.Files)
	}
	if progress.Sent != progress.Total {
		t.Errorf("sent = %d, total = %d, want equal", progress.Sent, progress.Total)
	}
	if result.progress.Files != 3 {
		t.Errorf("received files = %d, want 3", result.progress.Files)
	}
	if result.progress.Name != "Docs" {
		t.Errorf("received batch name = %q, want %q", result.progress.Name, "Docs")
	}
	if result.progress.Sent != progress.Total {
		t.Errorf("received sent = %d, want %d", result.progress.Sent, progress.Total)
	}

	for path, want := range map[string][]byte{
		filepath.Join(inbox, "Docs", "a.txt"):                top,
		filepath.Join(inbox, "Docs", "sub", "deep", "c.txt"): deep,
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if sha256.Sum256(got) != sha256.Sum256(want) {
			t.Errorf("received %s differs from the source", path)
		}
	}
	if _, err := os.Stat(filepath.Join(inbox, "Docs", "sub", "b.bin")); err != nil {
		t.Errorf("nested file is missing: %v", err)
	}
}

func TestPacerThrottles(t *testing.T) {
	ctx := context.Background()
	if newPacer(0) != nil {
		t.Fatal("a zero rate must disable the pacer")
	}
	if err := newPacer(0).wait(ctx, 1<<20); err != nil {
		t.Fatalf("disabled pacer returned %v", err)
	}

	limit := newPacer(200 * 1024)
	start := time.Now()
	if err := limit.wait(ctx, 100*1024); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond {
		t.Errorf("pacer waited only %v, want about 500ms", elapsed)
	}

	slow := newPacer(1024)
	cancelCtx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start = time.Now()
	if err := slow.wait(cancelCtx, 1024); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("cancel took %v, want the wait to be interrupted", elapsed)
	}
}

func TestSendHonoursBandwidthLimit(t *testing.T) {
	secret := randomSecret(t)
	cert, err := Certificate(secret)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}

	sender, receiver, _, receiverAddr := loopbackPair(t)

	dir := t.TempDir()
	source := filepath.Join(dir, "limited.bin")
	want := writeFile(t, source, 128*1024)
	inbox := filepath.Join(dir, "inbox")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	server, err := NewSession(receiver, cert)
	if err != nil {
		t.Fatalf("server session: %v", err)
	}
	defer server.Close()
	server.SetBandwidth(0, 256*1024)

	client, err := NewSession(sender, cert)
	if err != nil {
		t.Fatalf("client session: %v", err)
	}
	defer client.Close()
	client.SetBandwidth(256*1024, 0)

	type outcome struct {
		progress Progress
		err      error
	}
	received := make(chan outcome, 1)
	go func() {
		progress, err := server.Receive(ctx, inbox, nil)
		received <- outcome{progress, err}
	}()

	start := time.Now()
	_, sendErr := client.Send(ctx, receiverAddr, source, nil)
	elapsed := time.Since(start)
	result := <-received
	if sendErr != nil {
		t.Fatalf("Send: %v (receiver said: %v)", sendErr, result.err)
	}
	if result.err != nil {
		t.Fatalf("Receive: %v", result.err)
	}
	if elapsed < 350*time.Millisecond {
		t.Errorf("128 KiB at 256 KiB/s took %v, want at least 350ms", elapsed)
	}

	got, err := os.ReadFile(filepath.Join(inbox, "limited.bin"))
	if err != nil {
		t.Fatalf("read received file: %v", err)
	}
	if sha256.Sum256(got) != sha256.Sum256(want) {
		t.Error("throttled transfer produced a different file")
	}
}

func TestRelaySessionAcceptsKnownPeer(t *testing.T) {
	secret := randomSecret(t)
	cert, err := Certificate(secret)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}

	relayConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen relay: %v", err)
	}
	dialer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen dialer: %v", err)
	}
	t.Cleanup(func() {
		relayConn.Close()
		dialer.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resolve := func(net.Addr) (tls.Certificate, bool) {
		return cert, true
	}
	relay, err := NewRelaySession(relayConn, cert, resolve)
	if err != nil {
		t.Fatalf("relay session: %v", err)
	}
	defer relay.Close()

	dir := t.TempDir()
	source := filepath.Join(dir, "hub.bin")
	want := writeFile(t, source, 96*1024)
	inbox := filepath.Join(dir, "inbox")

	type outcome struct {
		progress Progress
		err      error
	}
	received := make(chan outcome, 1)
	go func() {
		progress, err := relay.Receive(ctx, inbox, nil)
		received <- outcome{progress, err}
	}()

	client, err := NewSession(dialer, cert)
	if err != nil {
		t.Fatalf("client session: %v", err)
	}
	defer client.Close()

	if _, err := client.Send(ctx, relayConn.LocalAddr(), source, nil); err != nil {
		t.Fatalf("Send over the hub: %v", err)
	}
	result := <-received
	if result.err != nil {
		t.Fatalf("Receive over the hub: %v", result.err)
	}
	if relay.Remote() == nil {
		t.Error("relay session must remember the remote address")
	}

	got, err := os.ReadFile(filepath.Join(inbox, "hub.bin"))
	if err != nil {
		t.Fatalf("read received file: %v", err)
	}
	if sha256.Sum256(got) != sha256.Sum256(want) {
		t.Error("file received over the hub differs from the source")
	}
}

func TestRelaySessionRejectsUnknownPeer(t *testing.T) {
	secret := randomSecret(t)
	cert, err := Certificate(secret)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}

	relayConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen relay: %v", err)
	}
	dialer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen dialer: %v", err)
	}
	t.Cleanup(func() {
		relayConn.Close()
		dialer.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	relay, err := NewRelaySession(relayConn, cert, func(net.Addr) (tls.Certificate, bool) {
		return tls.Certificate{}, false
	})
	if err != nil {
		t.Fatalf("relay session: %v", err)
	}
	defer relay.Close()

	go func() {
		_, _ = relay.Receive(ctx, filepath.Join(t.TempDir(), "inbox"), nil)
	}()

	dir := t.TempDir()
	source := filepath.Join(dir, "intruder.bin")
	writeFile(t, source, 4096)

	client, err := NewSession(dialer, cert)
	if err != nil {
		t.Fatalf("client session: %v", err)
	}
	defer client.Close()

	if _, err := client.Send(ctx, relayConn.LocalAddr(), source, nil); err == nil {
		t.Error("a peer that is not in the Alcea network must not be able to send")
	}
}

func TestCheckRel(t *testing.T) {
	valid := []string{"a.txt", "Docs/a.txt", "Docs/sub/deep/c.txt", "проект/файл.txt"}
	for _, rel := range valid {
		if err := checkRel(rel); err != nil {
			t.Errorf("checkRel(%q) = %v, want nil", rel, err)
		}
	}

	invalid := []string{"", ".", "..", "/abs.txt", "../evil.txt", "Docs/../../evil.txt", "a//b", `C:\Windows\x.txt`, "a/b\\c", "Docs/"}
	for _, rel := range invalid {
		if err := checkRel(rel); err == nil {
			t.Errorf("checkRel(%q) = nil, want an error", rel)
		}
	}
}
