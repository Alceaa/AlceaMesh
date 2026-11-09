package wgconf

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Interface Interface
	Peers     []Peer
}

type Interface struct {
	PrivateKey string
	Address    []string
	DNS        []string
	MTU        int
	ListenPort int
	FwMark     int
	Table      string
	SaveConfig bool
	PreUp      string
	PostUp     string
	PreDown    string
	PostDown   string
}

type Peer struct {
	PublicKey           string
	PresharedKey        string
	AllowedIPs          []string
	Endpoint            string
	PersistentKeepalive int
}

func ParseFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Config, error) {
	cfg := &Config{}
	section := ""
	peerIdx := -1

	for i, raw := range strings.Split(string(data), "\n") {
		lineNo := i + 1
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			switch strings.ToLower(name) {
			case "interface":
				section = "interface"
			case "peer":
				section = "peer"
				cfg.Peers = append(cfg.Peers, Peer{})
				peerIdx = len(cfg.Peers) - 1
			default:
				return nil, fmt.Errorf("line %d: unknown section [%s]", lineNo, name)
			}
			continue
		}

		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			return nil, fmt.Errorf("line %d: expected \"Key = Value\"", lineNo)
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if c := strings.IndexByte(val, '#'); c >= 0 {
			val = strings.TrimSpace(val[:c])
		}
		if err := assign(cfg, section, peerIdx, key, val, lineNo); err != nil {
			return nil, err
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func assign(cfg *Config, section string, peerIdx int, key, val string, line int) error {
	switch section {
	case "interface":
		iface := &cfg.Interface
		switch strings.ToLower(key) {
		case "privatekey":
			iface.PrivateKey = val
		case "address":
			iface.Address = appendList(iface.Address, val)
		case "dns":
			iface.DNS = appendList(iface.DNS, val)
		case "mtu":
			n, err := atoiField(val, "MTU", line)
			if err != nil {
				return err
			}
			iface.MTU = n
		case "listenport":
			n, err := atoiField(val, "ListenPort", line)
			if err != nil {
				return err
			}
			iface.ListenPort = n
		case "fwmark":
			n, err := atoiField(val, "FwMark", line)
			if err != nil {
				return err
			}
			iface.FwMark = n
		case "table":
			iface.Table = val
		case "saveconfig":
			iface.SaveConfig = strings.EqualFold(val, "true")
		case "preup":
			iface.PreUp = val
		case "postup":
			iface.PostUp = val
		case "predown":
			iface.PreDown = val
		case "postdown":
			iface.PostDown = val
		default:
			return fmt.Errorf("line %d: unknown [Interface] key %q", line, key)
		}
	case "peer":
		if peerIdx < 0 {
			return fmt.Errorf("line %d: key %q outside of a section", line, key)
		}
		peer := &cfg.Peers[peerIdx]
		switch strings.ToLower(key) {
		case "publickey":
			peer.PublicKey = val
		case "presharedkey":
			peer.PresharedKey = val
		case "allowedips":
			peer.AllowedIPs = appendList(peer.AllowedIPs, val)
		case "endpoint":
			peer.Endpoint = val
		case "persistentkeepalive":
			n, err := atoiField(val, "PersistentKeepalive", line)
			if err != nil {
				return err
			}
			peer.PersistentKeepalive = n
		default:
			return fmt.Errorf("line %d: unknown [Peer] key %q", line, key)
		}
	default:
		return fmt.Errorf("line %d: key %q outside of a section", line, key)
	}
	return nil
}

func (c *Config) Validate() error {
	if c.Interface.PrivateKey == "" {
		return fmt.Errorf("interface private key is required")
	}
	if err := validateKey("interface private key", c.Interface.PrivateKey); err != nil {
		return err
	}
	for _, addr := range c.Interface.Address {
		if _, _, err := net.ParseCIDR(addr); err != nil {
			return fmt.Errorf("invalid interface address %q: %w", addr, err)
		}
	}
	if c.Interface.ListenPort < 0 || c.Interface.ListenPort > 65535 {
		return fmt.Errorf("listen port out of range: %d", c.Interface.ListenPort)
	}

	for i := range c.Peers {
		peer := &c.Peers[i]
		if peer.PublicKey == "" {
			return fmt.Errorf("peer %d: public key is required", i+1)
		}
		if err := validateKey(fmt.Sprintf("peer %d public key", i+1), peer.PublicKey); err != nil {
			return err
		}
		if peer.PresharedKey != "" {
			if err := validateKey(fmt.Sprintf("peer %d preshared key", i+1), peer.PresharedKey); err != nil {
				return err
			}
		}
		for _, allowed := range peer.AllowedIPs {
			if _, _, err := net.ParseCIDR(allowed); err != nil {
				return fmt.Errorf("peer %d: invalid allowed ip %q: %w", i+1, allowed, err)
			}
		}
		if peer.PersistentKeepalive < 0 || peer.PersistentKeepalive > 65535 {
			return fmt.Errorf("peer %d: persistent keepalive out of range: %d", i+1, peer.PersistentKeepalive)
		}
	}
	return nil
}

func (c *Config) Marshal() []byte {
	var b strings.Builder
	iface := c.Interface

	b.WriteString("[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", iface.PrivateKey)
	for _, addr := range iface.Address {
		fmt.Fprintf(&b, "Address = %s\n", addr)
	}
	if len(iface.DNS) > 0 {
		fmt.Fprintf(&b, "DNS = %s\n", strings.Join(iface.DNS, ", "))
	}
	if iface.MTU > 0 {
		fmt.Fprintf(&b, "MTU = %d\n", iface.MTU)
	}
	if iface.ListenPort > 0 {
		fmt.Fprintf(&b, "ListenPort = %d\n", iface.ListenPort)
	}
	if iface.FwMark > 0 {
		fmt.Fprintf(&b, "FwMark = %d\n", iface.FwMark)
	}
	if iface.Table != "" {
		fmt.Fprintf(&b, "Table = %s\n", iface.Table)
	}
	if iface.SaveConfig {
		b.WriteString("SaveConfig = true\n")
	}
	if iface.PreUp != "" {
		fmt.Fprintf(&b, "PreUp = %s\n", iface.PreUp)
	}
	if iface.PostUp != "" {
		fmt.Fprintf(&b, "PostUp = %s\n", iface.PostUp)
	}
	if iface.PreDown != "" {
		fmt.Fprintf(&b, "PreDown = %s\n", iface.PreDown)
	}
	if iface.PostDown != "" {
		fmt.Fprintf(&b, "PostDown = %s\n", iface.PostDown)
	}

	for _, peer := range c.Peers {
		b.WriteString("\n[Peer]\n")
		fmt.Fprintf(&b, "PublicKey = %s\n", peer.PublicKey)
		if peer.PresharedKey != "" {
			fmt.Fprintf(&b, "PresharedKey = %s\n", peer.PresharedKey)
		}
		if len(peer.AllowedIPs) > 0 {
			fmt.Fprintf(&b, "AllowedIPs = %s\n", strings.Join(peer.AllowedIPs, ", "))
		}
		if peer.Endpoint != "" {
			fmt.Fprintf(&b, "Endpoint = %s\n", peer.Endpoint)
		}
		if peer.PersistentKeepalive > 0 {
			fmt.Fprintf(&b, "PersistentKeepalive = %d\n", peer.PersistentKeepalive)
		}
	}
	return []byte(b.String())
}

func (c *Config) ToUAPI() (string, error) {
	var b strings.Builder

	privHex, err := keyToHex(c.Interface.PrivateKey)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "private_key=%s\n", privHex)
	if c.Interface.ListenPort > 0 {
		fmt.Fprintf(&b, "listen_port=%d\n", c.Interface.ListenPort)
	}
	if c.Interface.FwMark > 0 {
		fmt.Fprintf(&b, "fwmark=%d\n", c.Interface.FwMark)
	}
	b.WriteString("replace_peers=true\n")

	for _, peer := range c.Peers {
		pubHex, err := keyToHex(peer.PublicKey)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "public_key=%s\n", pubHex)
		if peer.PresharedKey != "" {
			pskHex, err := keyToHex(peer.PresharedKey)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "preshared_key=%s\n", pskHex)
		}
		if peer.Endpoint != "" {
			fmt.Fprintf(&b, "endpoint=%s\n", peer.Endpoint)
		}
		if peer.PersistentKeepalive > 0 {
			fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", peer.PersistentKeepalive)
		}
		b.WriteString("replace_allowed_ips=true\n")
		for _, allowed := range peer.AllowedIPs {
			fmt.Fprintf(&b, "allowed_ip=%s\n", allowed)
		}
	}
	return b.String(), nil
}

func (c *Config) PublicKey() (string, error) {
	raw, err := base64.StdEncoding.DecodeString(c.Interface.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("invalid base64 private key: %w", err)
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("private key must be 32 bytes, got %d", len(raw))
	}
	private, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", fmt.Errorf("invalid private key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(private.PublicKey().Bytes()), nil
}

func (c *Config) PrimaryAddress() string {
	for _, value := range c.Interface.Address {
		if prefix, err := netip.ParsePrefix(value); err == nil {
			return prefix.Addr().String()
		}
		if addr, err := netip.ParseAddr(value); err == nil {
			return addr.String()
		}
	}
	return ""
}

func keyToHex(key string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return "", fmt.Errorf("invalid base64 key: %w", err)
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("key must be 32 bytes, got %d", len(raw))
	}
	return hex.EncodeToString(raw), nil
}

func validateKey(what, key string) error {
	if _, err := keyToHex(key); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

func appendList(dst []string, val string) []string {
	for _, part := range strings.Split(val, ",") {
		if part = strings.TrimSpace(part); part != "" {
			dst = append(dst, part)
		}
	}
	return dst
}

func atoiField(val, name string, line int) (int, error) {
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("line %d: invalid %s value %q", line, name, val)
	}
	return n, nil
}
