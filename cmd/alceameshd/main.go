package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"alceacore.ru/alceamesh/internal/daemon"
	"alceacore.ru/alceamesh/internal/ipc"
	"alceacore.ru/alceamesh/internal/wgengine"
)

const version = "0.4.0-m4"

func main() {
	if isWindowsService() {
		if err := runAsService(); err != nil {
			log.Println("service error:", err)
			os.Exit(1)
		}
		return
	}
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
		fmt.Printf("alceameshd %s\n", version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	case "run":
		return cmdRun(rest)
	case "install":
		return cmdInstall(rest)
	case "uninstall":
		return cmdUninstall(rest)
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `alceameshd - AlceaMesh privileged service

usage:
  alceameshd run        run in the foreground (for testing, requires admin)
  alceameshd install    register as a Windows service (requires admin)
  alceameshd uninstall  remove the Windows service (requires admin)
  alceameshd version

The service owns the WireGuard tunnels and listens on an IPC socket so that
unprivileged clients (CLI, GUI) can manage tunnels without administrator rights.
`)
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	verbose := fs.Bool("verbose", false, "verbose WireGuard logging")
	if err := fs.Parse(args); err != nil {
		return err
	}
	wgengine.SetVerbose(*verbose)

	listener, err := ipc.Listen()
	if err != nil {
		return fmt.Errorf("listen on %s: %w", ipc.SocketPath(), err)
	}
	defer listener.Close()

	log.Printf("alceameshd %s listening on %s", version, ipc.SocketPath())
	return serve(listener)
}

func serve(listener net.Listener) error {
	d := daemon.New(version)
	server := ipc.NewServer()
	d.Register(server)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("shutting down")
		listener.Close()
	}()

	err := server.Serve(listener)
	d.Shutdown()
	return err
}
