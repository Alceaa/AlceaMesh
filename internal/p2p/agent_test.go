package p2p

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"
)

type signalBox struct {
	mu    sync.Mutex
	boxes map[string][]Signal
}

func newSignalBox() *signalBox {
	return &signalBox{boxes: make(map[string][]Signal)}
}

func (b *signalBox) post(to string, signal Signal) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.boxes[to] = append(b.boxes[to], signal)
}

func (b *signalBox) take(me string) []Signal {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.boxes[me]
	delete(b.boxes, me)
	return out
}

type boxClient struct {
	box *signalBox
	me  string
}

func (c boxClient) PostSignal(_ context.Context, to string, signal Signal) error {
	c.box.post(to, signal)
	return nil
}

func (c boxClient) TakeSignals(context.Context) ([]Signal, error) {
	return c.box.take(c.me), nil
}

func startFakeSTUN(t *testing.T) string {
	t.Helper()

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			id, ok := bindingRequestID(buf[:n])
			if !ok {
				continue
			}
			addr, err := netip.ParseAddrPort(from.String())
			if err != nil {
				continue
			}
			_, _ = conn.WriteToUDP(buildBindingResponse(id, addr), from)
		}
	}()
	return conn.LocalAddr().String()
}

func stateOf(agent *Agent, peer string) State {
	for _, status := range agent.Statuses() {
		if status.Peer == peer {
			return status.State
		}
	}
	return StateIdle
}

func waitFor(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}

func TestAgentsEstablishDirectPath(t *testing.T) {
	stunServer := startFakeSTUN(t)
	box := newSignalBox()

	alice := NewAgent(Config{
		PublicKey:  "alice",
		Name:       "Alice",
		MeshIP:     "100.64.0.2",
		STUNServer: stunServer,
		Signaler:   boxClient{box: box, me: "alice"},
	})
	bob := NewAgent(Config{
		PublicKey:  "bob",
		Name:       "Bob",
		MeshIP:     "100.64.0.3",
		STUNServer: stunServer,
		Signaler:   boxClient{box: box, me: "bob"},
	})

	if err := alice.Start(); err != nil {
		t.Fatalf("alice start: %v", err)
	}
	defer alice.Close()
	if err := bob.Start(); err != nil {
		t.Fatalf("bob start: %v", err)
	}
	defer bob.Close()

	if !waitFor(5*time.Second, func() bool {
		return alice.PublicAddr().IsValid() && bob.PublicAddr().IsValid()
	}) {
		t.Fatalf("agents did not learn their public address: %s / %s", alice.PublicAddr(), bob.PublicAddr())
	}

	if err := alice.Connect(Peer{PublicKey: "bob", Name: "Bob", MeshIP: "100.64.0.3"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if stateOf(alice, "bob") == StateDirect && stateOf(bob, "alice") == StateDirect {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if got := stateOf(alice, "bob"); got != StateDirect {
		t.Errorf("alice sees bob as %q, want direct", got)
	}
	if got := stateOf(bob, "alice"); got != StateDirect {
		t.Errorf("bob sees alice as %q, want direct", got)
	}
}

func TestConnectRejectsDuplicateSession(t *testing.T) {
	stunServer := startFakeSTUN(t)
	box := newSignalBox()

	alice := NewAgent(Config{
		PublicKey:  "alice",
		Name:       "Alice",
		STUNServer: stunServer,
		Signaler:   boxClient{box: box, me: "alice"},
	})
	if err := alice.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer alice.Close()

	peer := Peer{PublicKey: "bob", Name: "Bob"}
	if err := alice.Connect(peer); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	if err := alice.Connect(peer); err == nil {
		t.Fatal("expected an error for a duplicate session")
	}
}

func TestDisconnectClosesSession(t *testing.T) {
	stunServer := startFakeSTUN(t)
	box := newSignalBox()

	alice := NewAgent(Config{
		PublicKey:  "alice",
		Name:       "Alice",
		STUNServer: stunServer,
		Signaler:   boxClient{box: box, me: "alice"},
	})
	if err := alice.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer alice.Close()

	if err := alice.Connect(Peer{PublicKey: "bob", Name: "Bob"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if len(alice.Statuses()) != 1 {
		t.Fatalf("expected one session, got %d", len(alice.Statuses()))
	}

	alice.Disconnect("bob")
	if len(alice.Statuses()) != 0 {
		t.Fatalf("expected no sessions after disconnect, got %d", len(alice.Statuses()))
	}
}

func TestSessionFallsBackToRelay(t *testing.T) {
	s := newSession(Peer{PublicKey: "bob", Name: "Bob"}, true, token{})
	s.mu.Lock()
	s.startedAt = time.Now().Add(-2 * dialTimeout)
	s.mu.Unlock()

	s.addCandidates([]netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:9")})
	s.refreshState()

	if got := s.status().State; got != StateRelay {
		t.Errorf("state = %q, want relay", got)
	}
}
