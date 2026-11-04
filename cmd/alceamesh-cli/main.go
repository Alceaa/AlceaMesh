package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"alceacore.ru/alceamesh/internal/wgconf"
	"alceacore.ru/alceamesh/internal/wgengine"
)

const version = "0.0.1-m0"

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
	case "show":
		return cmdShow(rest)
	case "uapi":
		return cmdUAPI(rest)
	case "up":
		return cmdUp(rest)
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `alceamesh-cli - AlceaMesh WireGuard proof of concept

usage:
  alceamesh-cli show  <config.conf>              parse and print a normalized config
  alceamesh-cli uapi  <config.conf>              print the UAPI payload sent to the engine
  alceamesh-cli up    [flags] <config.conf>      bring the tunnel up and stay in foreground
  alceamesh-cli version

flags for "up":
  --name NAME     adapter name (default: derived from the config file name)
  --verbose       enable verbose WireGuard logging
`)
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

func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	name := fs.String("name", "", "adapter name (default: derived from the config file name)")
	verbose := fs.Bool("verbose", false, "enable verbose WireGuard logging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: alceamesh-cli up [--name NAME] [--verbose] <config.conf>")
	}
	path := fs.Arg(0)

	cfg, err := wgconf.ParseFile(path)
	if err != nil {
		return err
	}
	if *name == "" {
		*name = adapterName(path)
	}
	wgengine.SetVerbose(*verbose)

	tunnel := wgengine.New(*name, cfg)
	if err := tunnel.Up(); err != nil {
		return err
	}

	fmt.Printf("tunnel %q is up\n", *name)
	fmt.Printf("  addresses: %s\n", strings.Join(cfg.Interface.Address, ", "))
	for i, peer := range cfg.Peers {
		fmt.Printf("  peer %d: endpoint=%s allowed_ips=%s\n", i+1, peer.Endpoint, strings.Join(peer.AllowedIPs, ", "))
	}
	fmt.Println("press Ctrl+C to stop")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	fmt.Println("\nshutting down...")
	return tunnel.Down()
}

func adapterName(path string) string {
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
