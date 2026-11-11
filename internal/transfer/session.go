package transfer

import (
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
)

type Session struct {
	transport  *quic.Transport
	cert       tls.Certificate
	listener   *quic.Listener
	resolve    RelayResolver
	remoteAddr atomic.Value

	mu          sync.Mutex
	sendRate    int64
	receiveRate int64
}

func NewSession(pc net.PacketConn, cert tls.Certificate) (*Session, error) {
	transport := &quic.Transport{Conn: pc}
	listener, err := transport.Listen(tlsConfig(cert), quicConfig())
	if err != nil {
		if closeErr := transport.Close(); closeErr != nil {
			return nil, fmt.Errorf("transfer: listen: %w", err)
		}
		return nil, fmt.Errorf("transfer: listen: %w", err)
	}
	return &Session{transport: transport, cert: cert, listener: listener}, nil
}

func (s *Session) SetBandwidth(sendBytesPerSecond, receiveBytesPerSecond int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendRate = sendBytesPerSecond
	s.receiveRate = receiveBytesPerSecond
}

func (s *Session) sendPacer() *pacer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return newPacer(s.sendRate)
}

func (s *Session) receivePacer() *pacer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return newPacer(s.receiveRate)
}

func (s *Session) Close() error {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	return s.transport.Close()
}

func quicConfig() *quic.Config {
	return &quic.Config{
		HandshakeIdleTimeout:           10 * time.Second,
		MaxIdleTimeout:                 60 * time.Second,
		KeepAlivePeriod:                20 * time.Second,
		InitialStreamReceiveWindow:     4 << 20,
		MaxStreamReceiveWindow:         16 << 20,
		InitialConnectionReceiveWindow: 8 << 20,
		MaxConnectionReceiveWindow:     64 << 20,
		MaxIncomingStreams:             16,
		DisablePathMTUDiscovery:        false,
	}
}
