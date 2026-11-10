package p2p

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"sort"
	"sync"
	"time"
)

const (
	SignalOffer  = "offer"
	SignalAnswer = "answer"

	defaultSTUNServer = "stun.l.google.com:19302"
	signalInterval    = time.Second
	signalTimeout     = 5 * time.Second
)

type Signal struct {
	From       string   `json:"from"`
	Kind       string   `json:"kind"`
	Token      string   `json:"token"`
	Candidates []string `json:"candidates"`
	MeshIP     string   `json:"mesh_ip"`
	Name       string   `json:"name"`
}

type Signaler interface {
	PostSignal(ctx context.Context, to string, signal Signal) error
	TakeSignals(ctx context.Context) ([]Signal, error)
}

type Config struct {
	PublicKey  string
	Name       string
	MeshIP     string
	STUNServer string
	Signaler   Signaler
}

type Agent struct {
	cfg    Config
	conn   *net.UDPConn
	public netip.AddrPort

	mu       sync.Mutex
	sessions map[string]*session
	waiting  map[transactionID]chan netip.AddrPort
	closed   bool

	closeCh chan struct{}
	wg      sync.WaitGroup
}

func NewAgent(cfg Config) *Agent {
	if cfg.STUNServer == "" {
		cfg.STUNServer = defaultSTUNServer
	}
	return &Agent{
		cfg:      cfg,
		sessions: make(map[string]*session),
		waiting:  make(map[transactionID]chan netip.AddrPort),
		closeCh:  make(chan struct{}),
	}
}

func (a *Agent) Start() error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return fmt.Errorf("p2p: bind udp socket: %w", err)
	}
	a.conn = conn

	if err := tuneSocket(conn); err != nil {
		log.Printf("p2p: socket tuning failed: %v", err)
	}

	a.wg.Add(1)
	go a.readLoop()

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := a.discover(); err != nil {
			log.Printf("p2p: stun discovery failed: %v", err)
		}
	}()

	a.wg.Add(1)
	go a.signalLoop()

	a.wg.Add(1)
	go a.punchLoop()
	return nil
}

func (a *Agent) Close() {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	conn := a.conn
	a.mu.Unlock()

	close(a.closeCh)
	if conn != nil {
		_ = conn.Close()
	}
	a.wg.Wait()
}

func (a *Agent) PublicAddr() netip.AddrPort {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.public
}

func (a *Agent) Connect(peer Peer) error {
	if peer.PublicKey == "" {
		return errors.New("p2p: peer public key is required")
	}
	if a.cfg.Signaler == nil {
		return errors.New("p2p: signaling is not configured")
	}

	tok, err := newToken()
	if err != nil {
		return err
	}

	a.mu.Lock()
	if _, exists := a.sessions[peer.PublicKey]; exists {
		a.mu.Unlock()
		return fmt.Errorf("p2p: session with %q already exists", peer.Name)
	}
	a.sessions[peer.PublicKey] = newSession(peer, true, tok)
	a.mu.Unlock()

	offer := Signal{
		Kind:       SignalOffer,
		From:       a.cfg.PublicKey,
		Name:       a.cfg.Name,
		MeshIP:     a.cfg.MeshIP,
		Token:      tok.String(),
		Candidates: a.candidateStrings(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), signalTimeout)
	defer cancel()
	if err := a.cfg.Signaler.PostSignal(ctx, peer.PublicKey, offer); err != nil {
		a.remove(peer.PublicKey)
		return fmt.Errorf("p2p: send offer: %w", err)
	}
	return nil
}

func (a *Agent) Disconnect(peerKey string) {
	if s := a.remove(peerKey); s != nil {
		if conn := s.dataConn(); conn != nil {
			_ = conn.Close()
		}
		a.sendTo(encodePacket(packet{kind: packetBye, token: s.token}), s.candidates())
	}
}

func (a *Agent) DataConn(peerKey string) (net.PacketConn, error) {
	s := a.session(peerKey)
	if s == nil {
		return nil, fmt.Errorf("p2p: no session with %q", peerKey)
	}
	remote := s.directAddr()
	if !remote.IsValid() {
		return nil, errors.New("p2p: the direct path is not established yet")
	}

	conn := newDataConn(a, peerKey, remote, 128)
	s.attachData(conn)
	return conn, nil
}

func (a *Agent) Statuses() []Status {
	sessions := a.snapshot()
	list := make([]Status, 0, len(sessions))
	for _, s := range sessions {
		list = append(list, s.status())
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].Peer < list[j].Peer
	})
	return list
}

func (a *Agent) snapshot() []*session {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		out = append(out, s)
	}
	return out
}

func (a *Agent) remove(peerKey string) *session {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[peerKey]
	if !ok {
		return nil
	}
	delete(a.sessions, peerKey)
	return s
}

func (a *Agent) session(peerKey string) *session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions[peerKey]
}

func (a *Agent) sendRaw(payload []byte, to netip.AddrPort) error {
	a.mu.Lock()
	conn := a.conn
	a.mu.Unlock()

	if conn == nil {
		return errors.New("p2p: agent is not running")
	}
	_, err := conn.WriteToUDP(payload, net.UDPAddrFromAddrPort(to))
	return err
}

func (a *Agent) localAddr() net.Addr {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn == nil {
		return &net.UDPAddr{}
	}
	return a.conn.LocalAddr()
}

func (a *Agent) isClosed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.closed
}

func (a *Agent) candidateStrings() []string {
	public := a.PublicAddr()
	if !public.IsValid() {
		return nil
	}
	return []string{public.String()}
}

func parseCandidates(values []string) []netip.AddrPort {
	out := make([]netip.AddrPort, 0, len(values))
	for _, value := range values {
		if addr, err := netip.ParseAddrPort(value); err == nil {
			out = append(out, addr)
		}
	}
	return out
}
