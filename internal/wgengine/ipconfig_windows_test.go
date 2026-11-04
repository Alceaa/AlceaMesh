//go:build windows

package wgengine

import (
	"net/netip"
	"testing"

	"alceacore.ru/alceamesh/internal/wgconf"
)

func TestAllowedIPRoutesNextHopFamily(t *testing.T) {
	cfg := &wgconf.Config{
		Peers: []wgconf.Peer{
			{AllowedIPs: []string{"10.0.0.0/24", "fd00::/64", "0.0.0.0/0", "::/0"}},
		},
	}
	routes := allowedIPRoutes(cfg)
	if len(routes) != 4 {
		t.Fatalf("got %d routes, want 4", len(routes))
	}
	for _, route := range routes {
		if !route.NextHop.IsValid() {
			t.Fatalf("route %s has an invalid next hop (this breaks CreateIpForwardEntry2)", route.Destination)
		}
		if route.NextHop.Is4() != route.Destination.Addr().Is4() {
			t.Errorf("route %s: next hop %s has a different family", route.Destination, route.NextHop)
		}
	}
}

func TestAllowedIPRoutesMasksAndDedupes(t *testing.T) {
	cfg := &wgconf.Config{
		Peers: []wgconf.Peer{
			{AllowedIPs: []string{"10.0.0.5/24", "fd00::1/64"}},
			{AllowedIPs: []string{"10.0.0.0/24"}},
		},
	}
	routes := allowedIPRoutes(cfg)
	if len(routes) != 2 {
		t.Fatalf("got %d routes, want 2 (10.0.0.0/24 deduped)", len(routes))
	}
	if routes[0].Destination != netip.MustParsePrefix("10.0.0.0/24") {
		t.Errorf("route[0] = %s, want 10.0.0.0/24 (masked)", routes[0].Destination)
	}
	if routes[0].NextHop != netip.IPv4Unspecified() {
		t.Errorf("route[0] next hop = %s, want 0.0.0.0", routes[0].NextHop)
	}
	if routes[1].Destination != netip.MustParsePrefix("fd00::/64") {
		t.Errorf("route[1] = %s, want fd00::/64", routes[1].Destination)
	}
	if routes[1].NextHop != netip.IPv6Unspecified() {
		t.Errorf("route[1] next hop = %s, want ::", routes[1].NextHop)
	}
}

func TestHasDefaultRoute(t *testing.T) {
	withDefault := allowedIPRoutes(&wgconf.Config{
		Peers: []wgconf.Peer{{AllowedIPs: []string{"0.0.0.0/0"}}},
	})
	if !hasDefaultRoute(withDefault) {
		t.Error("hasDefaultRoute = false, want true")
	}

	splitTunnel := allowedIPRoutes(&wgconf.Config{
		Peers: []wgconf.Peer{{AllowedIPs: []string{"10.0.0.0/24", "fd00::/64"}}},
	})
	if hasDefaultRoute(splitTunnel) {
		t.Error("hasDefaultRoute = true, want false")
	}
}

func TestTableOff(t *testing.T) {
	if !tableOff(&wgconf.Config{Interface: wgconf.Interface{Table: "off"}}) {
		t.Error("tableOff = false for Table = off")
	}
	if tableOff(&wgconf.Config{Interface: wgconf.Interface{Table: "auto"}}) {
		t.Error("tableOff = true for Table = auto")
	}
	if tableOff(&wgconf.Config{}) {
		t.Error("tableOff = true for empty Table")
	}
}
