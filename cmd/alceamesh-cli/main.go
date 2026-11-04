package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"alceacore.ru/alceamesh/internal/client"
	"alceacore.ru/alceamesh/internal/ipc"
	"alceacore.ru/alceamesh/internal/wgconf"
)

const version = "0.1.0-m1"

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
  alceamesh-cli version

Commands that talk to the service require it to be running (alceameshd run).
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
		tunnelName = tunnelNameFromPath(path)
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

func tunnelNameFromPath(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, base)
	if base == "" {
		return "AlceaMesh"
	}
	return base
}
