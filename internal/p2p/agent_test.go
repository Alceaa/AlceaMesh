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

func TestSessionFallsBackToRelayWithoutPackets(t *testing.T) {
	s := newSession(Peer{PublicKey: "bob", Name: "Bob"}, true, token{})
	s.addCandidates([]netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:9")})

	if got := s.status().State; got != StateProbing {
		t.Fatalf("state = %q, want probing", got)
	}

	s.mu.Lock()
	s.startedAt = time.Now().Add(-2 * dialTimeout)
	s.mu.Unlock()

	if !s.expire() {
		t.Fatal("a silent session must give up on the direct path")
	}

	status := s.status()
	if status.State != StateRelay {
		t.Errorf("state = %q, want relay", status.State)
	}
	if status.Message == "" {
		t.Error("the relay fallback must explain itself")
	}
	if s.expire() {
		t.Error("the session must not report the same deadline twice")
	}
}

func TestRelaySessionRetriesPunchWindows(t *testing.T) {
	s := newSession(Peer{PublicKey: "bob", Name: "Bob"}, true, token{})
	s.addCandidates([]netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:9")})

	s.mu.Lock()
	s.startedAt = time.Now().Add(-2 * dialTimeout)
	s.punchUntil = time.Now().Add(-time.Second)
	s.nextPunch = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if !s.expire() {
		t.Fatal("a silent session must fall back to relay")
	}

	if _, _, ok := s.outgoing(); !ok {
		t.Fatal("a relay session must retry the direct path")
	}
	if _, _, ok := s.outgoing(); !ok {
		t.Error("punches must repeat while the window is open")
	}

	s.mu.Lock()
	s.punchUntil = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if _, _, ok := s.outgoing(); ok {
		t.Error("the retry must wait for the interval after a failed window")
	}

	s.mu.Lock()
	s.nextPunch = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if _, _, ok := s.outgoing(); !ok {
		t.Error("a new punch window must open after the interval")
	}
}

func TestFirstPunchWindowEndsAtTheDeadline(t *testing.T) {
	s := newSession(Peer{PublicKey: "bob", Name: "Bob"}, true, token{})
	s.addCandidates([]netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:9")})

	s.mu.Lock()
	windowEnds := s.punchUntil
	retryAt := s.nextPunch
	s.mu.Unlock()

	if left := time.Until(windowEnds); left < dialTimeout-time.Second || left > dialTimeout+time.Second {
		t.Errorf("the first punch window lasts %s, want about %s", left, dialTimeout)
	}
	if !retryAt.After(windowEnds) {
		t.Error("the first retry must start after the punch window")
	}
	if _, _, ok := s.outgoing(); !ok {
		t.Error("the first punch must go out right away")
	}
}

func TestAgentTickMovesSilentSessionToRelay(t *testing.T) {
	stunServer := startFakeSTUN(t)
	box := newSignalBox()

	agent := NewAgent(Config{
		PublicKey:  "alice",
		Name:       "Alice",
		STUNServer: stunServer,
		Signaler:   boxClient{box: box, me: "alice"},
	})
	if err := agent.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer agent.Close()

	if err := agent.Connect(Peer{PublicKey: "bob", Name: "Bob", MeshIP: "100.64.0.3"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if got := stateOf(agent, "bob"); got != StateProbing {
		t.Fatalf("state = %q, want probing", got)
	}

	s := agent.session("bob")
	s.addCandidates([]netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:9")})
	s.mu.Lock()
	s.startedAt = time.Now().Add(-2 * dialTimeout)
	s.punchUntil = time.Now().Add(-time.Second)
	s.nextPunch = time.Now().Add(-time.Second)
	s.mu.Unlock()

	agent.tick()

	if got := stateOf(agent, "bob"); got != StateRelay {
		t.Errorf("state = %q, want relay", got)
	}
}

func TestStunServersPreferTheConfiguredServer(t *testing.T) {
	agent := NewAgent(Config{PublicKey: "alice", STUNServer: "127.0.0.1:3478"})
	servers := agent.stunServers()
	if len(servers) == 0 || servers[0] != "127.0.0.1:3478" {
		t.Fatalf("the configured server must come first, got %v", servers)
	}

	seen := make(map[string]bool, len(servers))
	for _, server := range servers {
		if seen[server] {
			t.Errorf("server %q is listed twice", server)
		}
		seen[server] = true
	}

	fallback := NewAgent(Config{PublicKey: "alice"}).stunServers()
	if len(fallback) != len(stunFallbacks) {
		t.Errorf("without a configured server the fallbacks must be used, got %v", fallback)
	}
}

func TestLocalCandidateStrings(t *testing.T) {
	if got := localCandidateStrings(0); len(got) != 0 {
		t.Errorf("a missing port must yield no candidates, got %v", got)
	}

	for _, value := range localCandidateStrings(41234) {
		addr, err := netip.ParseAddrPort(value)
		if err != nil {
			t.Errorf("candidate %q: %v", value, err)
			continue
		}
		if addr.Port() != 41234 {
			t.Errorf("candidate %q carries the wrong port", value)
		}
		if !addr.Addr().Is4() || addr.Addr().IsLoopback() {
			t.Errorf("candidate %q must be a routable IPv4 address", value)
		}
	}
}
