package ipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

func SocketPath() string {
	return filepath.Join(SocketDir(), "alceamesh.sock")
}

func Listen() (net.Listener, error) {
	dir := SocketDir()
	if err := prepareSocketDir(dir); err != nil {
		return nil, err
	}

	path := SocketPath()
	if err := removeStaleSocket(path); err != nil {
		return nil, err
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("%w (is another instance already running?)", err)
	}
	return listener, nil
}

func removeStaleSocket(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket %s: %w", path, err)
	}
	return nil
}

func DialPath(path string) (*Client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	return NewClient(conn), nil
}
