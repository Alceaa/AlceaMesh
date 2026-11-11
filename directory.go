package main

import (
	"net"
	"strings"
	"sync"
)

type peerEntry struct {
	Key    string
	Name   string
	MeshIP string
}

var (
	directoryMu   sync.RWMutex
	directoryMesh = make(map[string]peerEntry)
	directoryKey  = make(map[string]peerEntry)
)

func rememberDevices(devices []DeviceView) {
	directoryMu.Lock()
	defer directoryMu.Unlock()

	for _, device := range devices {
		entry := peerEntry{Key: device.PublicKey, Name: device.Name, MeshIP: device.MeshIP}
		if entry.MeshIP != "" {
			directoryMesh[entry.MeshIP] = entry
		}
		if entry.Key != "" {
			directoryKey[entry.Key] = entry
		}
	}
}

func peerByMeshIP(ip string) (peerEntry, bool) {
	directoryMu.RLock()
	defer directoryMu.RUnlock()

	entry, ok := directoryMesh[strings.TrimSpace(ip)]
	return entry, ok
}

func peerOfAddress(addr net.Addr) (peerEntry, bool) {
	if addr == nil {
		return peerEntry{}, false
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return peerEntry{}, false
	}
	return peerByMeshIP(host)
}

func meshIPOfPeer(peerKey string) string {
	directoryMu.RLock()
	defer directoryMu.RUnlock()

	return directoryKey[peerKey].MeshIP
}
