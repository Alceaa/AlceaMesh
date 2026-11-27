package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"alceacore.ru/alceamesh/internal/audit"
	"alceacore.ru/alceamesh/internal/coordinator"
	"alceacore.ru/alceamesh/internal/coordinator/api"
	"alceacore.ru/alceamesh/internal/coordinator/sqlitestore"
	"alceacore.ru/alceamesh/internal/identity"
	buildversion "alceacore.ru/alceamesh/internal/version"
)

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
		fmt.Printf("alceacoord %s\n", buildversion.String())
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	case "run":
		return cmdRun(rest)
	case "token":
		return cmdToken(rest)
	default:
		usage()
		return fmt.Errorf("unknown command %q", command)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `alceacoord - AlceaMesh coordinator (control plane for the VPS)

usage:
  alceacoord run   [--listen ADDR] [--db FILE] [--admin-key KEY] [--pool CIDR] [--presence-ttl DUR]
  alceacoord token [--db FILE] [--label TEXT] [--mesh-ip IP] [--ttl-hours N]
  alceacoord version

The coordinator keeps the device registry, hands out mesh IPs through enrollment
tokens and tracks presence. It never stores files. Run it on the VPS and expose
it to the mesh (a TLS reverse proxy in front is recommended).

The admin key defaults to the ALCEAMESH_ADMIN_KEY environment variable.
`)
}

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	listen := fs.String("listen", ":7788", "address to listen on")
	dbPath := fs.String("db", "alceacoord.db", "SQLite database file")
	adminKey := fs.String("admin-key", os.Getenv("ALCEAMESH_ADMIN_KEY"), "admin API key (env ALCEAMESH_ADMIN_KEY)")
	pool := fs.String("pool", coordinator.DefaultPool, "mesh IP pool (CIDR)")
	presenceTTL := fs.Duration("presence-ttl", coordinator.DefaultPresenceTTL, "presence timeout")
	legacyMode := fs.Bool("legacy-mode", true, "accept requests without a device signature (for clients older than the trust update)")
	tlsCert := fs.String("tls-cert", "", "TLS certificate (enables HTTPS, together with -tls-key)")
	tlsKey := fs.String("tls-key", "", "TLS private key")
	publicListen := fs.String("public-listen", "", "additional address for the public listener (registration, version, ping only)")
	publicHTTP := fs.Bool("public-http", false, "serve the public listener over plain HTTP (use when a TLS proxy such as Caddy is in front)")
	enrollRate := fs.Int("enroll-rate", 10, "registration attempts per minute per address (0 = unlimited)")
	enrollBurst := fs.Int("enroll-burst", 5, "how many registration attempts may happen back to back")
	auditLog := fs.String("audit-log", "", "file for security audit events (default: the service log)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store, err := sqlitestore.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("open store %s: %w", *dbPath, err)
	}
	defer store.Close()

	registry, err := coordinator.NewRegistry(store, coordinator.Options{
		Pool:        *pool,
		PresenceTTL: *presenceTTL,
	})
	if err != nil {
		return err
	}

	server := &api.Server{
		Registry:         registry,
		AdminKey:         *adminKey,
		Version:          buildversion.String(),
		RequireSignature: !*legacyMode,
		Nonces:           identity.NewNonceCache(identity.DefaultNonceTTL, identity.DefaultNonceLimit),
		Limiter:          api.NewLimiter(*enrollRate, *enrollBurst, 0),
		Audit:            audit.NewStdLogger(),
	}
	if *auditLog != "" {
		logger, closer, err := audit.NewFile(*auditLog)
		if err != nil {
			return err
		}
		defer closer.Close()
		server.Audit = logger
	}
	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	var publicServer *http.Server

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		log.Println("shutting down")
		_ = httpServer.Close()
		if publicServer != nil {
			_ = publicServer.Close()
		}
	}()

	if *adminKey == "" {
		log.Println("warning: no admin key set, token management is disabled")
	}
	if *legacyMode {
		log.Println("warning: legacy mode is on — requests without a device signature are accepted (fits clients older than the trust update; turn off with -legacy-mode=false)")
	}
	if (*tlsCert == "") != (*tlsKey == "") {
		return fmt.Errorf("tls-cert and tls-key must be set together")
	}

	if *publicListen != "" {
		publicServer = &http.Server{
			Addr:              *publicListen,
			Handler:           server.PublicHandler(),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			var err error
			if *tlsCert != "" && !*publicHTTP {
				err = publicServer.ListenAndServeTLS(*tlsCert, *tlsKey)
			} else {
				err = publicServer.ListenAndServe()
			}
			if err != nil && err != http.ErrServerClosed {
				log.Printf("public listener stopped: %v", err)
			}
		}()
		scheme := "https"
		if *publicHTTP {
			scheme = "http (за TLS-прокси)"
		}
		log.Printf("alceacoord public listener on %s — %s, отдаёт регистрацию, версию и ping", *publicListen, scheme)
	}

	scheme := "http"
	if *tlsCert != "" {
		scheme = "https"
	}
	log.Printf("alceacoord %s listening on %s (%s, db=%s, pool=%s, presence-ttl=%s)",
		buildversion.String(), *listen, scheme, *dbPath, *pool, *presenceTTL)

	if *tlsCert != "" {
		if err := httpServer.ListenAndServeTLS(*tlsCert, *tlsKey); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func cmdToken(args []string) error {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	dbPath := fs.String("db", "alceacoord.db", "SQLite database file")
	label := fs.String("label", "", "human-readable label for the token")
	meshIP := fs.String("mesh-ip", "", "pin the device to this mesh IP")
	ttlHours := fs.Int("ttl-hours", 0, "token lifetime in hours (0 = never expires)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store, err := sqlitestore.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("open store %s: %w", *dbPath, err)
	}
	defer store.Close()

	registry, err := coordinator.NewRegistry(store, coordinator.Options{})
	if err != nil {
		return err
	}

	var ttl time.Duration
	if *ttlHours > 0 {
		ttl = time.Duration(*ttlHours) * time.Hour
	}
	token, err := registry.CreateToken(*label, *meshIP, ttl)
	if err != nil {
		return err
	}
	fmt.Println(token.Value)
	return nil
}
