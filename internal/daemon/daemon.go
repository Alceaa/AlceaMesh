package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"

	"alceacore.ru/alceamesh/internal/ipc"
	"alceacore.ru/alceamesh/internal/wgconf"
	"alceacore.ru/alceamesh/internal/wgengine"
)

type Daemon struct {
	Version string

	mu      sync.Mutex
	tunnels map[string]*managedTunnel
}

type managedTunnel struct {
	cfg    *wgconf.Config
	tunnel *wgengine.Tunnel
}

func New(version string) *Daemon {
	return &Daemon{Version: version, tunnels: make(map[string]*managedTunnel)}
}

func (d *Daemon) Register(server *ipc.Server) {
	server.Handle(ipc.MethodPing, d.handlePing)
	server.Handle(ipc.MethodVersion, d.handleVersion)
	server.Handle(ipc.MethodStatus, d.handleStatus)
	server.Handle(ipc.MethodTunnelList, d.handleStatus)
	server.Handle(ipc.MethodTunnelUp, d.handleTunnelUp)
	server.Handle(ipc.MethodTunnelDown, d.handleTunnelDown)
}

func (d *Daemon) Shutdown() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for name, mt := range d.tunnels {
		_ = mt.tunnel.Down()
		delete(d.tunnels, name)
	}
}

func (d *Daemon) handlePing(_ json.RawMessage) (any, error) {
	return map[string]bool{"pong": true}, nil
}

func (d *Daemon) handleVersion(_ json.RawMessage) (any, error) {
	return ipc.VersionResult{Version: d.Version, PID: os.Getpid()}, nil
}

func (d *Daemon) handleStatus(_ json.RawMessage) (any, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	result := ipc.StatusResult{Version: d.Version}
	names := make([]string, 0, len(d.tunnels))
	for name := range d.tunnels {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		result.Tunnels = append(result.Tunnels, tunnelInfo(name, d.tunnels[name]))
	}
	return result, nil
}

func (d *Daemon) handleTunnelUp(params json.RawMessage) (any, error) {
	var p ipc.TunnelUpParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("tunnel name is required")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if mt, ok := d.tunnels[p.Name]; ok && mt.tunnel.Running() {
		return nil, fmt.Errorf("tunnel %q is already up", p.Name)
	}

	cfg, err := loadConfig(p)
	if err != nil {
		return nil, err
	}

	tunnel := wgengine.New(p.Name, cfg)
	if err := tunnel.Up(); err != nil {
		return nil, err
	}
	mt := &managedTunnel{cfg: cfg, tunnel: tunnel}
	d.tunnels[p.Name] = mt
	return tunnelInfo(p.Name, mt), nil
}

func (d *Daemon) handleTunnelDown(params json.RawMessage) (any, error) {
	var p ipc.TunnelDownParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	mt, ok := d.tunnels[p.Name]
	if !ok {
		return nil, fmt.Errorf("tunnel %q not found", p.Name)
	}
	if err := mt.tunnel.Down(); err != nil {
		return nil, err
	}
	delete(d.tunnels, p.Name)
	return map[string]bool{"ok": true}, nil
}

func loadConfig(p ipc.TunnelUpParams) (*wgconf.Config, error) {
	switch {
	case p.ConfigText != "":
		return wgconf.Parse([]byte(p.ConfigText))
	case p.ConfigPath != "":
		return wgconf.ParseFile(p.ConfigPath)
	default:
		return nil, fmt.Errorf("either config_path or config_text is required")
	}
}

func tunnelInfo(name string, mt *managedTunnel) ipc.TunnelInfo {
	info := ipc.TunnelInfo{
		Name:      name,
		Running:   mt.tunnel.Running(),
		Addresses: mt.cfg.Interface.Address,
		DNS:       mt.cfg.Interface.DNS,
		MTU:       mt.cfg.Interface.MTU,
	}
	for _, peer := range mt.cfg.Peers {
		info.Peers = append(info.Peers, ipc.PeerInfo{
			Endpoint:            peer.Endpoint,
			AllowedIPs:          peer.AllowedIPs,
			PersistentKeepalive: peer.PersistentKeepalive,
		})
	}
	return info
}
