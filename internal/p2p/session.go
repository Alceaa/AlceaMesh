package p2p

import (
	"net/netip"
	"sync"
	"time"
)

const (
	punchInterval     = 250 * time.Millisecond
	keepaliveInterval = 15 * time.Second
	dialTimeout       = 8 * time.Second
	retryInterval     = 45 * time.Second
	deadAfter         = 45 * time.Second
	maxCandidates     = 8

	relayMessage     = "direct path unavailable, using the Alcea hub"
	timeoutMessage   = "direct path timed out"
	closedByPeerText = "peer closed the connection"
)

type Peer struct {
	PublicKey string
	Name      string
	MeshIP    string
}

type State string

const (
	StateIdle    State = "idle"
	StateProbing State = "probing"
	StateDirect  State = "direct"
	StateRelay   State = "relay"
)

type Status struct {
	Peer     string `json:"peer"`
	Name     string `json:"name"`
	MeshIP   string `json:"meshIP"`
	State    State  `json:"state"`
	Endpoint string `json:"endpoint"`
	RTTms    int    `json:"rttMs"`
	Message  string `json:"message"`
}

type session struct {
	mu sync.Mutex

	peer      Peer
	initiator bool
	token     token
	remote    []netip.AddrPort

	state    State
	message  string
	sawPunch bool
	sawAck   bool
	lastSeen time.Time
	sentAt   time.Time
	rtt      time.Duration
	lastPing time.Time
	peerAddr netip.AddrPort
	data     *dataConn

	punchUntil time.Time
	nextPunch  time.Time

	startedAt time.Time
}

func newSession(peer Peer, initiator bool, tok token) *session {
	now := time.Now()
	return &session{
		peer:       peer,
		initiator:  initiator,
		token:      tok,
		state:      StateProbing,
		punchUntil: now.Add(dialTimeout),
		nextPunch:  now.Add(dialTimeout + retryInterval),
		startedAt:  now,
	}
}

func (s *session) addCandidates(addrs []netip.AddrPort) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, addr := range addrs {
		if !addr.IsValid() || addr.Port() == 0 {
			continue
		}
		known := false
		for _, existing := range s.remote {
			if existing == addr {
				known = true
				break
			}
		}
		if !known && len(s.remote) < maxCandidates {
			s.remote = append(s.remote, addr)
		}
	}
}

func (s *session) candidates() []netip.AddrPort {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]netip.AddrPort, len(s.remote))
	copy(out, s.remote)
	return out
}

func (s *session) outgoing() ([]byte, []netip.AddrPort, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == StateDirect {
		return nil, nil, false
	}
	if len(s.remote) == 0 {
		return nil, nil, false
	}

	now := time.Now()
	if !now.Before(s.punchUntil) {
		if now.Before(s.nextPunch) {
			return nil, nil, false
		}
		s.punchUntil = now.Add(dialTimeout)
		s.nextPunch = now.Add(retryInterval)
	}

	s.sentAt = now
	return encodePacket(packet{kind: packetPunch, token: s.token}), s.candidatesLocked(), true
}

func (s *session) candidatesLocked() []netip.AddrPort {
	out := make([]netip.AddrPort, len(s.remote))
	copy(out, s.remote)
	return out
}

func (s *session) keepalive() ([]byte, []netip.AddrPort) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateDirect {
		return nil, nil
	}
	if time.Since(s.lastPing) < keepaliveInterval {
		return nil, nil
	}
	s.lastPing = time.Now()
	s.sentAt = s.lastPing
	return encodePacket(packet{kind: packetPunch, token: s.token}), s.candidatesLocked()
}

func (s *session) handle(data []byte, from netip.AddrPort) []byte {
	p, err := decodePacket(data)
	if err != nil || p.token != s.token {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	switch p.kind {
	case packetPunch:
		s.sawPunch = true
		s.lastSeen = time.Now()
		s.peerAddr = from
		if !s.sentAt.IsZero() {
			s.rtt = time.Since(s.sentAt)
		}
		s.refreshState()
		return encodePacket(packet{kind: packetAck, token: s.token})

	case packetAck:
		s.sawAck = true
		s.lastSeen = time.Now()
		s.peerAddr = from
		if !s.sentAt.IsZero() {
			s.rtt = time.Since(s.sentAt)
		}
		s.refreshState()

	case packetBye:
		s.state = StateIdle
		s.message = closedByPeerText
	}
	return nil
}

func (s *session) refreshState() {
	if s.state == StateDirect {
		return
	}
	if s.sawPunch && s.sawAck {
		s.state = StateDirect
		s.message = ""
		return
	}
	if time.Since(s.startedAt) > dialTimeout {
		s.state = StateRelay
		s.message = relayMessage
	}
}

func (s *session) expire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch s.state {
	case StateProbing:
		if time.Since(s.startedAt) > dialTimeout {
			s.state = StateRelay
			s.message = relayMessage
			return true
		}
	case StateDirect:
		if s.lastSeen.IsZero() {
			return false
		}
		if time.Since(s.lastSeen) > deadAfter {
			s.state = StateRelay
			s.message = timeoutMessage
			s.sawPunch = false
			s.sawAck = false
			s.startedAt = time.Now()
			return true
		}
	}
	return false
}

func (s *session) status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	endpoint := ""
	if s.state == StateDirect && s.peerAddr.IsValid() {
		endpoint = s.peerAddr.String()
	}
	return Status{
		Peer:     s.peer.PublicKey,
		Name:     s.peer.Name,
		MeshIP:   s.peer.MeshIP,
		State:    s.state,
		Endpoint: endpoint,
		RTTms:    int(s.rtt.Milliseconds()),
		Message:  s.message,
	}
}

func (s *session) directAddr() netip.AddrPort {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateDirect || !s.peerAddr.IsValid() {
		return netip.AddrPort{}
	}
	return s.peerAddr
}

func (s *session) attachData(conn *dataConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = conn
}

func (s *session) dataConn() *dataConn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data
}
