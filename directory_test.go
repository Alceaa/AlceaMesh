package main

import (
	"net"
	"testing"
)

func TestLimitBytes(t *testing.T) {
	cases := []struct {
		kbps int
		want int64
	}{
		{0, 0},
		{-5, 0},
		{1, 1024},
		{512, 512 * 1024},
	}
	for _, item := range cases {
		if got := limitBytes(item.kbps); got != item.want {
			t.Errorf("limitBytes(%d) = %d, want %d", item.kbps, got, item.want)
		}
	}
}

func TestDirectoryLookup(t *testing.T) {
	rememberDevices([]DeviceView{
		{ID: "1", Name: "Ноутбук", MeshIP: "10.66.0.5", PublicKey: "key-bob"},
		{ID: "2", Name: "Домашний", MeshIP: "10.66.0.9", PublicKey: "key-carol"},
	})

	entry, ok := peerByMeshIP("10.66.0.5")
	if !ok || entry.Key != "key-bob" || entry.Name != "Ноутбук" {
		t.Fatalf("peerByMeshIP = %+v, ok = %v", entry, ok)
	}
	if got := meshIPOfPeer("key-carol"); got != "10.66.0.9" {
		t.Errorf("meshIPOfPeer = %q, want 10.66.0.9", got)
	}
	if _, ok := peerByMeshIP("8.8.8.8"); ok {
		t.Error("an unknown address must not resolve to a peer")
	}
	if got := meshIPOfPeer("nobody"); got != "" {
		t.Errorf("meshIPOfPeer(unknown) = %q, want an empty string", got)
	}

	addr := &net.UDPAddr{IP: net.IPv4(10, 66, 0, 5), Port: 51900}
	fromAddr, ok := peerOfAddress(addr)
	if !ok || fromAddr.Key != "key-bob" {
		t.Errorf("peerOfAddress = %+v, ok = %v", fromAddr, ok)
	}
	if _, ok := peerOfAddress(&net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 51900}); ok {
		t.Error("a public address must not resolve to a mesh peer")
	}
	if _, ok := peerOfAddress(nil); ok {
		t.Error("a nil address must not resolve to a mesh peer")
	}
}
