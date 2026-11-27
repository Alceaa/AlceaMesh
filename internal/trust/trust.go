package trust

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

const (
	ServerName  = "mesh.alceacore.ru"
	MeshAddress = "10.66.66.1:7443"
)

var rootPEM = `
-----BEGIN CERTIFICATE-----
MIIBWzCCAQ2gAwIBAgIQMZ5OT/AOwQwZEV8qyupgJjAFBgMrZXAwLDEOMAwGA1UE
ChMFQWxjZWExGjAYBgNVBAMTEUFsY2VhTWVzaCBSb290IENBMB4XDTI2MTEyNzEx
MDE0MloXDTM2MTEyNzEyMDE0MlowLDEOMAwGA1UEChMFQWxjZWExGjAYBgNVBAMT
EUFsY2VhTWVzaCBSb290IENBMCowBQYDK2VwAyEANkPaZAmVP+/PqOgeBRl02d/l
538ETKY7VkZSVGFb/zqjRTBDMA4GA1UdDwEB/wQEAwIBBjASBgNVHRMBAf8ECDAG
AQH/AgEAMB0GA1UdDgQWBBSRlqoKUKycIkeVnyY9IjvCkv6siDAFBgMrZXADQQBY
9xe4HaswYMawO1vYmPNDiXXDtWsNv8DYFI4E9sB2ikKX5Ed6G57v/gJuUbJ0BG7p
mibH735MXKatS+V0ZJAJ
-----END CERTIFICATE-----
`
var rootPin = "uyDHVcy9KposXo2kKfkSz1UtOPJ0F0a00MoE9Fi137U="

var previousRootPEM = ``
var previousRootPin = ``

func Pinned() bool {
	return hasCertificate(rootPEM) && strings.TrimSpace(rootPin) != ""
}

func RootCertificate() (*x509.Certificate, error) {
	return parseCertificate(rootPEM, "корневой сертификат не закреплён в этой сборке")
}

func PreviousRootCertificate() (*x509.Certificate, error) {
	if !hasCertificate(previousRootPEM) {
		return nil, errors.New("trust: previous root is not pinned")
	}
	return parseCertificate(previousRootPEM, "предыдущий корень не закреплён")
}

func PinOf(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:])
}

func VerifyPin() error {
	cert, err := parseCertificate(rootPEM, "корневой сертификат не закреплён в этой сборке")
	if err != nil {
		return err
	}
	if strings.TrimSpace(rootPin) == "" {
		return errors.New("trust: отпечаток корня не закреплён в этой сборке")
	}
	if got := PinOf(cert); got != strings.TrimSpace(rootPin) {
		return fmt.Errorf("trust: отпечаток корня не совпал с закреплённым (получено %s)", got)
	}
	if hasCertificate(previousRootPEM) {
		legacy, err := PreviousRootCertificate()
		if err != nil {
			return err
		}
		if strings.TrimSpace(previousRootPin) == "" {
			return errors.New("trust: предыдущий корень закреплён без отпечатка")
		}
		if got := PinOf(legacy); got != strings.TrimSpace(previousRootPin) {
			return fmt.Errorf("trust: отпечаток предыдущего корня не совпал (получено %s)", got)
		}
	}
	return nil
}

func RootPool() (*x509.CertPool, error) {
	cert, err := parseCertificate(rootPEM, "корневой сертификат не закреплён в этой сборке")
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)

	if hasCertificate(previousRootPEM) {
		legacy, err := PreviousRootCertificate()
		if err != nil {
			return nil, err
		}
		pool.AddCert(legacy)
	}
	return pool, nil
}

func TLSConfig() (*tls.Config, error) {
	return tlsConfig(rootPEM, rootPin, previousRootPEM, previousRootPin, ServerName)
}

func tlsConfig(certPEM, expectedPin, legacyPEM, legacyPin, serverName string) (*tls.Config, error) {
	cert, err := parseCertificate(certPEM, "корневой сертификат не закреплён в этой сборке")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(expectedPin) == "" {
		return nil, errors.New("trust: не закреплён отпечаток корня (rootPin) — приложение откажется подключаться, чтобы не доверять случайному сертификату")
	}
	if got := PinOf(cert); got != strings.TrimSpace(expectedPin) {
		return nil, fmt.Errorf("trust: отпечаток корня не совпал с закреплённым (получено %s)", got)
	}

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	if hasCertificate(legacyPEM) {
		legacy, err := parseCertificate(legacyPEM, "предыдущий корень не читается")
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(legacyPin) == "" || PinOf(legacy) != strings.TrimSpace(legacyPin) {
			return nil, errors.New("trust: отпечаток предыдущего корня не совпал")
		}
		pool.AddCert(legacy)
	}

	return &tls.Config{
		RootCAs:    pool,
		ServerName: serverName,
		MinVersion: tls.VersionTLS13,
	}, nil
}

func RootPinInstructions() string {
	return `Чтобы закрепить корень:
1) выполните: alceamesh-cli ca init --dir ./ca
2) скопируйте содержимое ./ca/root.crt в переменную rootPEM в internal/trust/root.go
3) скопируйте отпечаток (команда alceamesh-cli ca pin --dir ./ca) в переменную rootPin там же
4) соберите приложение заново`
}

func hasCertificate(value string) bool {
	return strings.Contains(value, "BEGIN CERTIFICATE")
}

func parseCertificate(value, missing string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("trust: %s (%s)", missing, RootPinInstructions())
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("trust: закреплённый сертификат не читается: %w", err)
	}
	return cert, nil
}
