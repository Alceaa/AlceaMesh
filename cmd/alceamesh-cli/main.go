package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"alceacore.ru/alceamesh/internal/client"
	"alceacore.ru/alceamesh/internal/coordclient"
	"alceacore.ru/alceamesh/internal/ipc"
	"alceacore.ru/alceamesh/internal/naming"
	"alceacore.ru/alceamesh/internal/wgconf"
)

const version = "0.3.0-m3"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("no command given")
	}
	command, rest := args[0], args[1:]
	switch command {
	case "version", "-v", "--version":
		fmt.Printf("alceamesh-cli %s\n", version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	case "ping":
		return cmdPing()
	case "status":
		return cmdStatus()
	case "show":
		return cmdShow(rest)
	case "uapi":
		return cmdUAPI(rest)
	case "up":
		return cmdUp(rest)
	case "down":
		return cmdDown(rest)
	case "coord":
		return cmdCoord(rest)
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `alceamesh-cli - AlceaMesh command line client

usage:
  alceamesh-cli ping                          check the service
  alceamesh-cli status                        show tunnels managed by the service
  alceamesh-cli up   [--name NAME] <conf>     bring a tunnel up via the service
  alceamesh-cli down <name>                   bring a tunnel down via the service
  alceamesh-cli show <conf>                   parse and print a normalized config (offline)
  alceamesh-cli uapi <conf>                   print the UAPI payload (offline)
  alceamesh-cli coord <sub> [flags]           talk to the coordinator (control plane)
  alceamesh-cli version

Coordinator subcommands: ping, version, devices, enroll, token, tokens, delete.
  alceamesh-cli coord enroll --token T --name NAME [--platform P] [--public-key PK]
  alceamesh-cli coord devices [--secret S | --admin-key K]
  alceamesh-cli coord token   --admin-key K [--label L] [--mesh-ip IP] [--ttl-hours N]

Commands that talk to the service require it to be running (alceameshd run).
Coordinator commands need --server (env ALCEAMESH_COORDINATOR).
`)
}

func cmdPing() error {
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()

	v, err := c.Version()
	if err != nil {
		return err
	}
	fmt.Printf("pong: version=%s pid=%d\n", v.Version, v.PID)
	return nil
}

func cmdStatus() error {
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()

	result, err := c.Status()
	if err != nil {
		return err
	}
	fmt.Printf("service version: %s\n", result.Version)
	if len(result.Tunnels) == 0 {
		fmt.Println("no tunnels")
		return nil
	}
	for _, tunnel := range result.Tunnels {
		printTunnel(tunnel)
	}
	return nil
}

func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	name := fs.String("name", "", "tunnel name (default: derived from the config file name)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: alceamesh-cli up [--name NAME] <config.conf>")
	}
	path := fs.Arg(0)

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if _, err := wgconf.Parse(data); err != nil {
		return fmt.Errorf("invalid config %s: %w", path, err)
	}

	tunnelName := *name
	if tunnelName == "" {
		tunnelName = naming.TunnelFromPath(path)
	}

	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()

	info, err := c.TunnelUp(ipc.TunnelUpParams{Name: tunnelName, ConfigText: string(data)})
	if err != nil {
		return err
	}
	printTunnel(*info)
	return nil
}

func cmdDown(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: alceamesh-cli down <name>")
	}
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()

	if err := c.TunnelDown(args[0]); err != nil {
		return err
	}
	fmt.Printf("tunnel %q is down\n", args[0])
	return nil
}

func cmdShow(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: alceamesh-cli show <config.conf>")
	}
	cfg, err := wgconf.ParseFile(args[0])
	if err != nil {
		return err
	}
	fmt.Print(string(cfg.Marshal()))
	return nil
}

func cmdUAPI(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: alceamesh-cli uapi <config.conf>")
	}
	cfg, err := wgconf.ParseFile(args[0])
	if err != nil {
		return err
	}
	uapi, err := cfg.ToUAPI()
	if err != nil {
		return err
	}
	fmt.Print(uapi)
	return nil
}

func dial() (*client.Client, error) {
	c, err := client.Dial()
	if err != nil {
		return nil, fmt.Errorf("cannot reach the AlceaMesh service: %w (is \"alceameshd run\" running?)", err)
	}
	return c, nil
}

func printTunnel(tunnel ipc.TunnelInfo) {
	state := "down"
	if tunnel.Running {
		state = "up"
	}
	fmt.Printf("tunnel %q [%s]\n", tunnel.Name, state)
	if len(tunnel.Addresses) > 0 {
		fmt.Printf("  addresses: %s\n", strings.Join(tunnel.Addresses, ", "))
	}
	if tunnel.MTU > 0 {
		fmt.Printf("  mtu: %d\n", tunnel.MTU)
	}
	if len(tunnel.DNS) > 0 {
		fmt.Printf("  dns: %s\n", strings.Join(tunnel.DNS, ", "))
	}
	for i, peer := range tunnel.Peers {
		fmt.Printf("  peer %d: endpoint=%s allowed_ips=%s\n", i+1, peer.Endpoint, strings.Join(peer.AllowedIPs, ", "))
	}
}

type coordFlags struct {
	server   string
	secret   string
	adminKey string
}

func newCoordFlagSet(name string) (*flag.FlagSet, *coordFlags) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	flags := &coordFlags{}
	fs.StringVar(&flags.server, "server", os.Getenv("ALCEAMESH_COORDINATOR"), "coordinator base URL (env ALCEAMESH_COORDINATOR)")
	fs.StringVar(&flags.secret, "secret", os.Getenv("ALCEAMESH_DEVICE_SECRET"), "device secret (env ALCEAMESH_DEVICE_SECRET)")
	fs.StringVar(&flags.adminKey, "admin-key", os.Getenv("ALCEAMESH_ADMIN_KEY"), "admin key (env ALCEAMESH_ADMIN_KEY)")
	return fs, flags
}

func (f *coordFlags) client(auth string) (*coordclient.Client, error) {
	if f.server == "" {
		return nil, fmt.Errorf("coordinator URL is required (--server or ALCEAMESH_COORDINATOR)")
	}
	return coordclient.New(f.server, auth), nil
}

func cmdCoord(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: alceamesh-cli coord <ping|version|devices|enroll|token|tokens|delete> [flags]")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "ping":
		return cmdCoordPing(rest)
	case "version":
		return cmdCoordVersion(rest)
	case "devices":
		return cmdCoordDevices(rest)
	case "enroll":
		return cmdCoordEnroll(rest)
	case "token":
		return cmdCoordToken(rest)
	case "tokens":
		return cmdCoordTokens(rest)
	case "delete":
		return cmdCoordDelete(rest)
	default:
		return fmt.Errorf("unknown coord subcommand %q", sub)
	}
}

func cmdCoordPing(args []string) error {
	fs, flags := newCoordFlagSet("coord ping")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := flags.client(flags.secret)
	if err != nil {
		return err
	}
	if err := c.Ping(); err != nil {
		return err
	}
	fmt.Println("coordinator: pong")
	return nil
}

func cmdCoordVersion(args []string) error {
	fs, flags := newCoordFlagSet("coord version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := flags.client("")
	if err != nil {
		return err
	}
	value, err := c.Version()
	if err != nil {
		return err
	}
	fmt.Printf("coordinator version: %s\n", value)
	return nil
}

func cmdCoordDevices(args []string) error {
	fs, flags := newCoordFlagSet("coord devices")
	if err := fs.Parse(args); err != nil {
		return err
	}
	auth := flags.secret
	if auth == "" {
		auth = flags.adminKey
	}
	c, err := flags.client(auth)
	if err != nil {
		return err
	}
	devices, err := c.Devices()
	if err != nil {
		return err
	}
	if len(devices) == 0 {
		fmt.Println("no devices")
		return nil
	}
	fmt.Printf("%-20s %-16s %-8s %s\n", "NAME", "MESH IP", "STATE", "PLATFORM")
	for _, device := range devices {
		state := "offline"
		if device.Online {
			state = "online"
		}
		fmt.Printf("%-20s %-16s %-8s %s\n", device.Name, device.MeshIP, state, device.Platform)
	}
	return nil
}

func cmdCoordEnroll(args []string) error {
	fs, flags := newCoordFlagSet("coord enroll")
	token := fs.String("token", "", "enrollment token (required)")
	name := fs.String("name", "", "device name (required)")
	platform := fs.String("platform", runtime.GOOS, "device platform")
	publicKey := fs.String("public-key", "", "WireGuard public key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *token == "" || *name == "" {
		return fmt.Errorf("usage: alceamesh-cli coord enroll --token T --name NAME [--platform P] [--public-key PK]")
	}
	c, err := flags.client("")
	if err != nil {
		return err
	}
	result, err := c.Enroll(*token, *name, *publicKey, *platform)
	if err != nil {
		return err
	}
	fmt.Printf("device %q enrolled\n", result.Device.Name)
	fmt.Printf("  id:      %s\n", result.Device.ID)
	fmt.Printf("  mesh ip: %s\n", result.Device.MeshIP)
	fmt.Printf("  secret:  %s\n", result.Secret)
	fmt.Println("store the secret: it is the device credential for heartbeats and the device list.")
	return nil
}

func cmdCoordToken(args []string) error {
	fs, flags := newCoordFlagSet("coord token")
	label := fs.String("label", "", "token label")
	meshIP := fs.String("mesh-ip", "", "pin the device to this mesh IP")
	ttlHours := fs.Int("ttl-hours", 0, "token lifetime in hours (0 = never expires)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if flags.adminKey == "" {
		return fmt.Errorf("admin key is required (--admin-key or ALCEAMESH_ADMIN_KEY)")
	}
	c, err := flags.client(flags.adminKey)
	if err != nil {
		return err
	}
	var ttl time.Duration
	if *ttlHours > 0 {
		ttl = time.Duration(*ttlHours) * time.Hour
	}
	token, err := c.CreateToken(*label, *meshIP, ttl)
	if err != nil {
		return err
	}
	fmt.Println(token.Value)
	return nil
}

func cmdCoordTokens(args []string) error {
	fs, flags := newCoordFlagSet("coord tokens")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if flags.adminKey == "" {
		return fmt.Errorf("admin key is required (--admin-key or ALCEAMESH_ADMIN_KEY)")
	}
	c, err := flags.client(flags.adminKey)
	if err != nil {
		return err
	}
	tokens, err := c.Tokens()
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		fmt.Println("no tokens")
		return nil
	}
	for _, token := range tokens {
		state := "unused"
		if token.UsedBy != "" {
			state = "used by " + token.UsedBy
		}
		fmt.Printf("%s  %-20s label=%q mesh_ip=%s\n", token.Value, state, token.Label, token.MeshIP)
	}
	return nil
}

func cmdCoordDelete(args []string) error {
	fs, flags := newCoordFlagSet("coord delete")
	id := fs.String("id", "", "device id (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if flags.adminKey == "" {
		return fmt.Errorf("admin key is required (--admin-key or ALCEAMESH_ADMIN_KEY)")
	}
	if *id == "" {
		return fmt.Errorf("usage: alceamesh-cli coord delete --id DEVICE_ID --admin-key K")
	}
	c, err := flags.client(flags.adminKey)
	if err != nil {
		return err
	}
	if err := c.DeleteDevice(*id); err != nil {
		return err
	}
	fmt.Printf("device %q removed\n", *id)
	return nil
}
