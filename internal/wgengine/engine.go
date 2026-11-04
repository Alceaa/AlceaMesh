package wgengine

import (
	"fmt"
	"net/netip"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"

	"alceacore.ru/alceamesh/internal/wgconf"
)

var LogLevel = device.LogLevelError

func SetVerbose(enabled bool) {
	if enabled {
		LogLevel = device.LogLevelVerbose
		return
	}
	LogLevel = device.LogLevelError
}

type Tunnel struct {
	Name   string
	Config *wgconf.Config

	mu      sync.Mutex
	tunDev  tun.Device
	dev     *device.Device
	luid    uint64
	running bool

	endpointRoutes  []netip.Prefix
	endpointNextHop netip.Addr
	endpointLUID    uint64
}

func New(name string, cfg *wgconf.Config) *Tunnel {
	return &Tunnel{Name: name, Config: cfg}
}

func (t *Tunnel) Up() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return fmt.Errorf("tunnel %q is already up", t.Name)
	}

	mtu := t.Config.Interface.MTU
	if mtu <= 0 {
		mtu = device.DefaultMTU
	}

	tunDev, err := tun.CreateTUN(t.Name, mtu)
	if err != nil {
		return fmt.Errorf("create adapter %q: %w", t.Name, err)
	}

	if err := t.configureInterface(tunDev, mtu); err != nil {
		tunDev.Close()
		return fmt.Errorf("configure interface: %w", err)
	}

	dev := device.NewDevice(tunDev, conn.NewDefaultBind(), device.NewLogger(LogLevel, fmt.Sprintf("[%s] ", t.Name)))

	uapi, err := t.Config.ToUAPI()
	if err != nil {
		dev.Close()
		return fmt.Errorf("build uapi config: %w", err)
	}
	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return fmt.Errorf("apply config: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return fmt.Errorf("bring device up: %w", err)
	}

	t.tunDev = tunDev
	t.dev = dev
	t.running = true
	return nil
}

func (t *Tunnel) Down() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return nil
	}

	_ = t.deconfigureInterface()
	if t.dev != nil {
		t.dev.Close()
	}
	t.tunDev = nil
	t.dev = nil
	t.running = false
	return nil
}

func (t *Tunnel) Running() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

func (t *Tunnel) IpcGet() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.running {
		return "", fmt.Errorf("tunnel %q is not running", t.Name)
	}
	return t.dev.IpcGet()
}
