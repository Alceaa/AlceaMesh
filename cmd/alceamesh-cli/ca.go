package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"alceacore.ru/alceamesh/internal/pki"
	"alceacore.ru/alceamesh/internal/trust"
)

const caPassphraseEnv = "ALCEAMESH_CA_PASSPHRASE"

func cmdCA(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: alceamesh-cli ca <init|issue|pin> [flags]")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "init":
		return cmdCAInit(rest)
	case "issue":
		return cmdCAIssue(rest)
	case "pin":
		return cmdCAPin(rest)
	default:
		return fmt.Errorf("unknown ca subcommand %q", sub)
	}
}

func passphraseFrom(flagValue string) (string, error) {
	if flagValue != "" {
		data, err := os.ReadFile(flagValue)
		if err != nil {
			return "", fmt.Errorf("read passphrase file: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	if value := strings.TrimSpace(os.Getenv(caPassphraseEnv)); value != "" {
		return value, nil
	}
	return "", fmt.Errorf("нужен пароль для корневого ключа: задайте %s или --passphrase-file FILE", caPassphraseEnv)
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func cmdCAInit(args []string) error {
	fs := flag.NewFlagSet("ca init", flag.ContinueOnError)
	dir := fs.String("dir", "", "folder for the CA files (required)")
	commonName := fs.String("common-name", "AlceaMesh Root CA", "subject common name")
	years := fs.Int("years", pki.RootYears, "root lifetime in years")
	passphraseFile := fs.String("passphrase-file", "", "file with the passphrase for the root key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("usage: alceamesh-cli ca init --dir ./ca [--common-name NAME] [--years N] [--passphrase-file FILE]")
	}
	passphrase, err := passphraseFrom(*passphraseFile)
	if err != nil {
		return err
	}

	root, err := pki.InitCA(pki.Options{
		Dir:        *dir,
		CommonName: *commonName,
		Passphrase: passphrase,
		Years:      *years,
	})
	if err != nil {
		return err
	}
	pin, err := pki.Pin(root.Cert)
	if err != nil {
		return err
	}

	fmt.Println("корень создан")
	fmt.Printf("  сертификат:   %s\n", filepath.Join(*dir, pki.RootCertFile))
	fmt.Printf("  ключ:         %s (зашифрован паролем — храните офлайн)\n", filepath.Join(*dir, pki.RootKeyFile))
	fmt.Printf("  отпечаток:    %s\n", pin)
	fmt.Printf("  действует до: %s\n", root.Cert.NotAfter.Format("2006-01-02"))
	fmt.Println()
	fmt.Println("Дальше:")
	fmt.Printf("  1) выпустить серверный сертификат:\n     alceamesh-cli ca issue --dir %s --dns %s --ip 10.66.66.1\n", *dir, trust.ServerName)
	fmt.Printf("  2) закрепить корень в приложении:\n     alceamesh-cli ca pin --dir %s   (напечатает готовый блок для internal/trust/root.go)\n", *dir)
	fmt.Println("  3) сделать две копии папки CA: потеря корневого ключа означает новую сборку всех клиентов")
	return nil
}

func cmdCAIssue(args []string) error {
	fs := flag.NewFlagSet("ca issue", flag.ContinueOnError)
	dir := fs.String("dir", "", "folder with the CA files (required)")
	dns := fs.String("dns", trust.ServerName, "comma-separated DNS names for the certificate")
	ips := fs.String("ip", "10.66.66.1", "comma-separated IP addresses for the certificate")
	years := fs.Int("years", pki.ServerYears, "certificate lifetime in years")
	passphraseFile := fs.String("passphrase-file", "", "file with the passphrase for the root key")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("usage: alceamesh-cli ca issue --dir ./ca [--dns NAME] [--ip ADDR] [--years N]")
	}
	passphrase, err := passphraseFrom(*passphraseFile)
	if err != nil {
		return err
	}

	names := splitList(*dns)
	var addresses []net.IP
	for _, value := range splitList(*ips) {
		parsed := net.ParseIP(value)
		if parsed == nil {
			return fmt.Errorf("%q is not an IP address", value)
		}
		addresses = append(addresses, parsed)
	}

	root, err := pki.OpenCA(*dir, passphrase)
	if err != nil {
		return err
	}
	issued, err := root.IssueServerTo(*dir, pki.ServerOptions{
		DNSNames: names,
		IPs:      addresses,
		Years:    *years,
	})
	if err != nil {
		return err
	}

	certPath := filepath.Join(*dir, pki.ServerCertFile)
	keyPath := filepath.Join(*dir, pki.ServerKeyFile)
	fmt.Println("серверный сертификат выпущен")
	fmt.Printf("  сертификат:   %s\n", certPath)
	fmt.Printf("  ключ:         %s\n", keyPath)
	fmt.Printf("  имена:        %s\n", strings.Join(names, ", "))
	fmt.Printf("  адреса:       %s\n", *ips)
	fmt.Printf("  действует до: %s\n", issued.Certificate.NotAfter.Format("2006-01-02"))
	fmt.Println()
	fmt.Println("Запуск координатора:")
	fmt.Printf("  alceacoord run --listen %s --tls-cert %s --tls-key %s --public-listen :8443\n",
		trust.MeshAddress, certPath, keyPath)
	fmt.Println("Проверка:")
	fmt.Printf("  openssl s_client -connect %s -servername %s -CAfile %s\n",
		trust.MeshAddress, trust.ServerName, filepath.Join(*dir, pki.RootCertFile))
	return nil
}

func cmdCAPin(args []string) error {
	fs := flag.NewFlagSet("ca pin", flag.ContinueOnError)
	dir := fs.String("dir", "", "folder with the CA files (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return fmt.Errorf("usage: alceamesh-cli ca pin --dir ./ca")
	}

	certPEM, err := os.ReadFile(filepath.Join(*dir, pki.RootCertFile))
	if err != nil {
		return fmt.Errorf("read root certificate: %w", err)
	}
	cert, err := pki.ParseCertificate(certPEM)
	if err != nil {
		return err
	}
	pin, err := pki.Pin(cert)
	if err != nil {
		return err
	}

	fmt.Printf("отпечаток корня (для rootPin):\n%s\n\n", pin)
	fmt.Println("Вставьте в internal/trust/root.go:")
	fmt.Println("var rootPEM = `")
	fmt.Print(string(certPEM))
	fmt.Println("`")
	fmt.Printf("var rootPin = %q\n", pin)
	fmt.Println()
	fmt.Printf("Корень действует до %s\n", cert.NotAfter.Format("2006-01-02"))
	fmt.Println("После вставки: go test ./internal/trust/ — тест проверит, что отпечаток сходится")
	return nil
}
