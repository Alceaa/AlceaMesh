package transfer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"

	"github.com/quic-go/quic-go"
)

type RelayResolver func(remote net.Addr) (tls.Certificate, bool)

func NewRelaySession(pc net.PacketConn, local tls.Certificate, resolve RelayResolver) (*Session, error) {
	transport := &quic.Transport{Conn: pc}
	listener, err := transport.Listen(relayTLSConfig(local, resolve), quicConfig())
	if err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("transfer: listen: %w", err)
	}
	return &Session{transport: transport, cert: local, listener: listener, resolve: resolve}, nil
}

func relayTLSConfig(local tls.Certificate, resolve RelayResolver) *tls.Config {
	config := &tls.Config{
		Certificates: []tls.Certificate{local},
		NextProtos:   []string{alpn},
	}
	config.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		if hello.Conn == nil {
			return nil, errors.New("transfer: peer address is unknown")
		}
		expected, ok := resolve(hello.Conn.RemoteAddr())
		if !ok {
			return nil, errors.New("transfer: peer is not in the Alcea network")
		}
		return tlsConfig(expected), nil
	}
	return config
}

func (s *Session) Remote() net.Addr {
	value := s.remoteAddr.Load()
	if value == nil {
		return nil
	}
	addr, _ := value.(net.Addr)
	return addr
}

func (s *Session) setRemote(addr net.Addr) {
	s.remoteAddr.Store(addr)
}
