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
	deadAfter         = 45 * time.Second
	maxCandidates     = 8
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

	startedAt time.Time
}

func newSession(peer Peer, initiator bool, tok token) *session {
	return &session{
		peer:      peer,
		initiator: initiator,
		token:     tok,
		state:     StateProbing,
		startedAt: time.Now(),
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
	if time.Since(s.startedAt) > dialTimeout {
		return nil, nil, false
	}
	if len(s.remote) == 0 {
		return nil, nil, false
	}
	s.sentAt = time.Now()
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
		if !s.sentAt.IsZero() {
			s.rtt = time.Since(s.sentAt)
		}
		s.refreshState()
		return encodePacket(packet{kind: packetAck, token: s.token})

	case packetAck:
		s.sawAck = true
		s.lastSeen = time.Now()
		if !s.sentAt.IsZero() {
			s.rtt = time.Since(s.sentAt)
		}
		s.refreshState()

	case packetBye:
		s.state = StateIdle
		s.message = "peer closed the connection"
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
		s.message = "direct path unavailable, using the Alcea hub"
	}
}

func (s *session) expire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateDirect || s.lastSeen.IsZero() {
		return false
	}
	if time.Since(s.lastSeen) > deadAfter {
		s.state = StateRelay
		s.message = "direct path timed out"
		s.sawPunch = false
		s.sawAck = false
		s.startedAt = time.Now()
		return true
	}
	return false
}

func (s *session) status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	endpoint := ""
	if s.state == StateDirect && len(s.remote) > 0 {
		endpoint = s.remote[0].String()
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
