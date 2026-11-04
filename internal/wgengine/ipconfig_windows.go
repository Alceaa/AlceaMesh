//go:build windows

package wgengine

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"strings"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/tun"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"

	"alceacore.ru/alceamesh/internal/wgconf"
)

func (t *Tunnel) configureInterface(tunDev tun.Device, mtu int) error {
	native, ok := tunDev.(*tun.NativeTun)
	if !ok {
		return fmt.Errorf("unexpected tun device type %T", tunDev)
	}
	luid := winipcfg.LUID(native.LUID())
	t.luid = uint64(luid)

	addresses, err := parsePrefixes(t.Config.Interface.Address)
	if err != nil {
		return err
	}
	routes := allowedIPRoutes(t.Config)
	defaultRoute := hasDefaultRoute(routes)

	for _, family := range []winipcfg.AddressFamily{windows.AF_INET, windows.AF_INET6} {
		if err := luid.SetIPAddressesForFamily(family, addresses); err != nil {
			return fmt.Errorf("set ip addresses: %w", err)
		}
		if !tableOff(t.Config) {
			if err := luid.SetRoutesForFamily(family, routes); err != nil {
				return fmt.Errorf("set routes: %w", err)
			}
		}
		if err := setIPInterface(luid, family, mtu, defaultRoute); err != nil {
			return fmt.Errorf("configure ip interface: %w", err)
		}
	}

	if err := t.preserveEndpointRoutes(defaultRoute); err != nil {
		log.Printf("alceamesh: warning: could not pin endpoint routes: %v", err)
	}

	if err := setDNS(luid, t.Config.Interface.DNS); err != nil {
		return fmt.Errorf("set dns: %w", err)
	}
	return nil
}

func (t *Tunnel) deconfigureInterface() error {
	t.removeEndpointRoutes()
	if t.luid == 0 {
		return nil
	}
	luid := winipcfg.LUID(t.luid)
	luid.FlushRoutes(windows.AF_UNSPEC)
	luid.FlushIPAddresses(windows.AF_UNSPEC)
	luid.FlushDNS(windows.AF_INET)
	luid.FlushDNS(windows.AF_INET6)
	return nil
}

func parsePrefixes(values []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("parse address %q: %w", value, err)
		}
		prefixes = append(prefixes, prefix)
	}
	return prefixes, nil
}

func setIPInterface(luid winipcfg.LUID, family winipcfg.AddressFamily, mtu int, defaultRoute bool) error {
	row, err := luid.IPInterface(family)
	if err != nil {
		return err
	}
	if mtu > 0 {
		row.NLMTU = uint32(mtu)
	}
	row.RouterDiscoveryBehavior = winipcfg.RouterDiscoveryDisabled
	row.DadTransmits = 0
	row.ManagedAddressConfigurationSupported = false
	row.OtherStatefulConfigurationSupported = false
	if defaultRoute {
		row.UseAutomaticMetric = false
		row.Metric = 0
	}
	return row.Set()
}

func tableOff(cfg *wgconf.Config) bool {
	return strings.EqualFold(cfg.Interface.Table, "off")
}

func hasDefaultRoute(routes []*winipcfg.RouteData) bool {
	for _, route := range routes {
		if route.Destination.Bits() == 0 {
			return true
		}
	}
	return false
}

func setDNS(luid winipcfg.LUID, values []string) error {
	if len(values) == 0 {
		return nil
	}
	servers := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		addr, err := netip.ParseAddr(value)
		if err != nil {
			return fmt.Errorf("parse dns %q: %w", value, err)
		}
		servers = append(servers, addr)
	}
	var firstErr error
	for _, family := range []winipcfg.AddressFamily{windows.AF_INET, windows.AF_INET6} {
		if err := luid.SetDNS(family, servers, nil); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func allowedIPRoutes(cfg *wgconf.Config) []*winipcfg.RouteData {
	var routes []*winipcfg.RouteData
	seen := make(map[netip.Prefix]bool)
	for _, peer := range cfg.Peers {
		for _, allowed := range peer.AllowedIPs {
			prefix, err := netip.ParsePrefix(allowed)
			if err != nil {
				continue
			}
			prefix = prefix.Masked()
			if seen[prefix] {
				continue
			}
			seen[prefix] = true

			route := &winipcfg.RouteData{Destination: prefix, Metric: 0}
			switch {
			case prefix.Addr().Is4():
				route.NextHop = netip.IPv4Unspecified()
			case prefix.Addr().Is6():
				route.NextHop = netip.IPv6Unspecified()
			default:
				continue
			}
			routes = append(routes, route)
		}
	}
	return routes
}

func (t *Tunnel) preserveEndpointRoutes(defaultRoute bool) error {
	if !defaultRoute {
		return nil
	}
	nextHop, gatewayLUID, ok := defaultRouteV4()
	if !ok {
		return errors.New("no IPv4 default route found")
	}
	var firstErr error
	seen := make(map[netip.Addr]bool)
	for _, peer := range t.Config.Peers {
		addr, ok := resolveEndpointV4(peer.Endpoint)
		if !ok || seen[addr] {
			continue
		}
		seen[addr] = true

		prefix := netip.PrefixFrom(addr, 32)
		err := gatewayLUID.AddRoute(prefix, nextHop, 0)
		if err == nil {
			t.endpointRoutes = append(t.endpointRoutes, prefix)
			t.endpointNextHop = nextHop
			t.endpointLUID = uint64(gatewayLUID)
			continue
		}
		if errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (t *Tunnel) removeEndpointRoutes() {
	if len(t.endpointRoutes) == 0 || t.endpointLUID == 0 {
		return
	}
	luid := winipcfg.LUID(t.endpointLUID)
	for _, prefix := range t.endpointRoutes {
		_ = luid.DeleteRoute(prefix, t.endpointNextHop)
	}
	t.endpointRoutes = nil
	t.endpointNextHop = netip.Addr{}
	t.endpointLUID = 0
}

func defaultRouteV4() (netip.Addr, winipcfg.LUID, bool) {
	table, err := winipcfg.GetIPForwardTable2(windows.AF_INET)
	if err != nil {
		return netip.Addr{}, 0, false
	}
	anyPrefix := netip.MustParsePrefix("0.0.0.0/0")
	bestMetric := ^uint32(0)
	var bestNextHop netip.Addr
	var bestLUID winipcfg.LUID
	for i := range table {
		row := &table[i]
		if row.DestinationPrefix.Prefix() != anyPrefix {
			continue
		}
		nextHop := row.NextHop.Addr()
		if !nextHop.IsValid() || nextHop.IsUnspecified() {
			continue
		}
		if row.Metric < bestMetric {
			bestMetric = row.Metric
			bestNextHop = nextHop
			bestLUID = row.InterfaceLUID
		}
	}
	if !bestNextHop.IsValid() {
		return netip.Addr{}, 0, false
	}
	return bestNextHop, bestLUID, true
}

func resolveEndpointV4(endpoint string) (netip.Addr, bool) {
	if endpoint == "" {
		return netip.Addr{}, false
	}
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		host = endpoint
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.Is4() {
			return addr, true
		}
		return netip.Addr{}, false
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return netip.Addr{}, false
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			if addr, ok := netip.AddrFromSlice(v4); ok {
				return addr, true
			}
		}
	}
	return netip.Addr{}, false
}
