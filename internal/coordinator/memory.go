package coordinator

import (
	"sort"
	"sync"
)

type MemoryStore struct {
	mu      sync.RWMutex
	devices map[string]Device
	tokens  map[string]Token
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		devices: make(map[string]Device),
		tokens:  make(map[string]Token),
	}
}

func (s *MemoryStore) PutDevice(device Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.devices[device.ID] = device
	return nil
}

func (s *MemoryStore) GetDevice(id string) (Device, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	device, ok := s.devices[id]
	return device, ok, nil
}

func (s *MemoryStore) GetDeviceBySecret(secret string) (Device, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, device := range s.devices {
		if secret != "" && device.Secret == secret {
			return device, true, nil
		}
	}
	return Device{}, false, nil
}

func (s *MemoryStore) GetDeviceByPublicKey(publicKey string) (Device, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, device := range s.devices {
		if publicKey != "" && device.PublicKey == publicKey {
			return device, true, nil
		}
	}
	return Device{}, false, nil
}

func (s *MemoryStore) ListDevices() ([]Device, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Device, 0, len(s.devices))
	for _, device := range s.devices {
		out = append(out, device)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *MemoryStore) DeleteDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.devices, id)
	return nil
}

func (s *MemoryStore) PutToken(token Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token.Value] = token
	return nil
}

func (s *MemoryStore) GetToken(value string) (Token, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	token, ok := s.tokens[value]
	return token, ok, nil
}

func (s *MemoryStore) ListTokens() ([]Token, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Token, 0, len(s.tokens))
	for _, token := range s.tokens {
		out = append(out, token)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].Value < out[j].Value
	})
	return out, nil
}
