//go:build windows

package p2p

import (
	"net"

	"golang.org/x/sys/windows"
)

const socketBufferSize = 4 << 20

func tuneSocket(conn *net.UDPConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}

	var tuneErr error
	if err := raw.Control(func(fd uintptr) {
		handle := windows.Handle(fd)
		if err := windows.SetsockoptInt(handle, windows.SOL_SOCKET, windows.SO_RCVBUF, socketBufferSize); err != nil {
			tuneErr = err
			return
		}
		if err := windows.SetsockoptInt(handle, windows.SOL_SOCKET, windows.SO_SNDBUF, socketBufferSize); err != nil {
			tuneErr = err
		}
	}); err != nil {
		return err
	}
	return tuneErr
}
