package p2p

import (
	"net"
	"net/netip"
	"os"
	"sync"
	"time"
)

type datagram struct {
	data []byte
	from netip.AddrPort
}

type dataConn struct {
	agent  *Agent
	peer   string
	remote netip.AddrPort
	inbox  chan datagram
	done   chan struct{}

	once sync.Once
	mu   sync.Mutex
	read time.Time
}

func newDataConn(agent *Agent, peer string, remote netip.AddrPort, buffer int) *dataConn {
	return &dataConn{
		agent:  agent,
		peer:   peer,
		remote: remote,
		inbox:  make(chan datagram, buffer),
		done:   make(chan struct{}),
	}
}

func (c *dataConn) push(d datagram) {
	select {
	case c.inbox <- d:
	case <-c.done:
	default:
	}
}

func (c *dataConn) ReadFrom(p []byte) (int, net.Addr, error) {
	c.mu.Lock()
	deadline := c.read
	c.mu.Unlock()

	var timeout <-chan time.Time
	if !deadline.IsZero() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		timeout = timer.C
	}

	select {
	case d := <-c.inbox:
		return copy(p, d.data), net.UDPAddrFromAddrPort(d.from), nil
	case <-timeout:
		return 0, nil, os.ErrDeadlineExceeded
	case <-c.done:
		return 0, nil, net.ErrClosed
	}
}

func (c *dataConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	select {
	case <-c.done:
		return 0, net.ErrClosed
	default:
	}
	if err := c.agent.sendRaw(p, c.remote); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *dataConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func (c *dataConn) LocalAddr() net.Addr {
	return c.agent.localAddr()
}

func (c *dataConn) SetDeadline(t time.Time) error {
	if err := c.SetReadDeadline(t); err != nil {
		return err
	}
	return c.SetWriteDeadline(t)
}

func (c *dataConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.read = t
	return nil
}

func (c *dataConn) SetWriteDeadline(time.Time) error {
	return nil
}
