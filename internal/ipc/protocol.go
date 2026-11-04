package ipc

import (
	"bufio"
	"encoding/json"
	"io"
)

const (
	MethodPing       = "ping"
	MethodVersion    = "version"
	MethodStatus     = "status"
	MethodTunnelUp   = "tunnel.up"
	MethodTunnelDown = "tunnel.down"
	MethodTunnelList = "tunnel.list"
)

type Request struct {
	ID     uint64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	ID     uint64          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type VersionResult struct {
	Version string `json:"version"`
	PID     int    `json:"pid"`
}

type StatusResult struct {
	Version string       `json:"version"`
	Tunnels []TunnelInfo `json:"tunnels"`
}

type TunnelInfo struct {
	Name      string     `json:"name"`
	Running   bool       `json:"running"`
	Addresses []string   `json:"addresses,omitempty"`
	DNS       []string   `json:"dns,omitempty"`
	MTU       int        `json:"mtu,omitempty"`
	Peers     []PeerInfo `json:"peers,omitempty"`
}

type PeerInfo struct {
	Endpoint            string   `json:"endpoint,omitempty"`
	AllowedIPs          []string `json:"allowed_ips,omitempty"`
	PersistentKeepalive int      `json:"persistent_keepalive,omitempty"`
}

type TunnelUpParams struct {
	Name       string `json:"name"`
	ConfigPath string `json:"config_path,omitempty"`
	ConfigText string `json:"config_text,omitempty"`
}

type TunnelDownParams struct {
	Name string `json:"name"`
}

func writeMessage(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

func readMessage(r *bufio.Reader, value any) error {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return err
	}
	return json.Unmarshal(line, value)
}
