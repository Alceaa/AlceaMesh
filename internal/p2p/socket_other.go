//go:build !windows

package p2p

import "net"

func tuneSocket(_ *net.UDPConn) error {
	return nil
}
