package coordinator

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

const (
	DefaultPresenceTTL = 90 * time.Second
	DefaultPool        = "10.0.0.0/24"
)

type Options struct {
	Pool        string
	PresenceTTL time.Duration
	Now         func() time.Time
}

type Registry struct {
	store Store
	pool  netip.Prefix
	ttl   time.Duration
	now   func() time.Time
}

func NewRegistry(store Store, opts Options) (*Registry, error) {
	poolValue := opts.Pool
	if poolValue == "" {
		poolValue = DefaultPool
	}
	pool, err := netip.ParsePrefix(poolValue)
	if err != nil {
		return nil, fmt.Errorf("parse pool %q: %w", poolValue, err)
	}
	ttl := opts.PresenceTTL
	if ttl <= 0 {
		ttl = DefaultPresenceTTL
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Registry{store: store, pool: pool.Masked(), ttl: ttl, now: now}, nil
}

func (r *Registry) PresenceTTL() time.Duration {
	return r.ttl
}

func (r *Registry) CreateToken(label, meshIP string, ttl time.Duration) (*Token, error) {
	if meshIP != "" {
		if _, err := netip.ParseAddr(meshIP); err != nil {
			return nil, fmt.Errorf("invalid mesh ip %q: %w", meshIP, err)
		}
	}
	value, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	now := r.now()
	token := Token{Value: value, Label: label, MeshIP: meshIP, CreatedAt: now}
	if ttl > 0 {
		token.ExpiresAt = now.Add(ttl)
	}
	if err := r.store.PutToken(token); err != nil {
		return nil, err
	}
	return &token, nil
}

func (r *Registry) Tokens() ([]Token, error) {
	return r.store.ListTokens()
}

func (r *Registry) Enroll(req EnrollRequest) (*Device, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("device name is required")
	}

	token, ok, err := r.store.GetToken(req.Token)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("unknown enrollment token")
	}
	now := r.now()
	if token.Expired(now) {
		return nil, fmt.Errorf("enrollment token has expired")
	}
	if token.Used() {
		return nil, fmt.Errorf("enrollment token has already been used")
	}

	meshIP, err := r.assignMeshIP(token.MeshIP)
	if err != nil {
		return nil, err
	}

	id, err := randomHex(8)
	if err != nil {
		return nil, err
	}
	secret, err := randomHex(32)
	if err != nil {
		return nil, err
	}

	device := Device{
		ID:         id,
		Secret:     secret,
		Name:       name,
		PublicKey:  strings.TrimSpace(req.PublicKey),
		MeshIP:     meshIP,
		Platform:   strings.TrimSpace(req.Platform),
		EnrolledAt: now,
		LastSeen:   now,
	}
	if err := r.store.PutDevice(device); err != nil {
		return nil, err
	}

	token.UsedBy = device.ID
	if err := r.store.PutToken(token); err != nil {
		return nil, err
	}
	return &device, nil
}

func (r *Registry) Authenticate(secret string) (*Device, error) {
	if secret == "" {
		return nil, fmt.Errorf("missing device secret")
	}
	device, ok, err := r.store.GetDeviceBySecret(secret)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("unknown device")
	}
	return &device, nil
}

func (r *Registry) Heartbeat(secret string) (*Device, error) {
	device, err := r.Authenticate(secret)
	if err != nil {
		return nil, err
	}
	device.LastSeen = r.now()
	if err := r.store.PutDevice(*device); err != nil {
		return nil, err
	}
	return device, nil
}

func (r *Registry) Devices() ([]DeviceView, error) {
	devices, err := r.store.ListDevices()
	if err != nil {
		return nil, err
	}
	now := r.now()
	views := make([]DeviceView, 0, len(devices))
	for _, device := range devices {
		views = append(views, DeviceView{Device: device, Online: r.online(device, now)})
	}
	return views, nil
}

func (r *Registry) Announce(req AnnounceRequest) (*Device, error) {
	publicKey := strings.TrimSpace(req.PublicKey)
	if publicKey == "" {
		return nil, fmt.Errorf("public key is required")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "device"
	}

	now := r.now()
	device, found, err := r.store.GetDeviceByPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	if !found {
		id, err := randomHex(8)
		if err != nil {
			return nil, err
		}
		secret, err := randomHex(32)
		if err != nil {
			return nil, err
		}
		device = Device{ID: id, Secret: secret, PublicKey: publicKey, EnrolledAt: now}
	}

	device.Name = name
	device.Platform = strings.TrimSpace(req.Platform)
	device.Available = req.Available
	device.LastSeen = now

	meshIP, err := r.resolveMeshIP(req.MeshIP, device.ID, device.MeshIP)
	if err != nil {
		return nil, err
	}
	device.MeshIP = meshIP

	if err := r.store.PutDevice(device); err != nil {
		return nil, err
	}
	return &device, nil
}

func (r *Registry) resolveMeshIP(requested, exceptID, current string) (string, error) {
	if requested != "" {
		used, err := r.ipInUse(requested, exceptID)
		if err != nil {
			return "", err
		}
		if !used {
			return requested, nil
		}
	}
	if current != "" {
		return current, nil
	}
	return r.nextFreeIP()
}

func (r *Registry) Peers(availableOnly bool) ([]DeviceView, error) {
	devices, err := r.store.ListDevices()
	if err != nil {
		return nil, err
	}
	now := r.now()
	views := make([]DeviceView, 0, len(devices))
	for _, device := range devices {
		if availableOnly && !device.Available {
			continue
		}
		views = append(views, DeviceView{Device: device, Online: r.online(device, now)})
	}
	return views, nil
}

func (r *Registry) DeleteDevice(id string) error {
	return r.store.DeleteDevice(id)
}

func (r *Registry) online(device Device, now time.Time) bool {
	return !device.LastSeen.IsZero() && now.Sub(device.LastSeen) < r.ttl
}

func (r *Registry) assignMeshIP(requested string) (string, error) {
	if requested != "" {
		used, err := r.ipInUse(requested, "")
		if err != nil {
			return "", err
		}
		if used {
			return "", fmt.Errorf("mesh ip %s is already in use", requested)
		}
		return requested, nil
	}
	return r.nextFreeIP()
}

func (r *Registry) nextFreeIP() (string, error) {
	devices, err := r.store.ListDevices()
	if err != nil {
		return "", err
	}
	used := make(map[netip.Addr]bool, len(devices))
	for _, device := range devices {
		if addr, err := netip.ParseAddr(device.MeshIP); err == nil {
			used[addr] = true
		}
	}

	network := r.pool.Addr()
	gateway := true
	for addr := network; r.pool.Contains(addr); addr = addr.Next() {
		if addr == network {
			continue
		}
		if !r.pool.Contains(addr.Next()) {
			break
		}
		if gateway {
			gateway = false
			continue
		}
		if used[addr] {
			continue
		}
		return addr.String(), nil
	}
	return "", fmt.Errorf("no free addresses in pool %s", r.pool)
}

func (r *Registry) ipInUse(meshIP, exceptID string) (bool, error) {
	devices, err := r.store.ListDevices()
	if err != nil {
		return false, err
	}
	for _, device := range devices {
		if device.ID != exceptID && device.MeshIP == meshIP {
			return true, nil
		}
	}
	return false, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
