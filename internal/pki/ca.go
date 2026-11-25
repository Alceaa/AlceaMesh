package pki

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"net"
	"time"
)

const (
	RootYears    = 10
	ServerYears  = 1
	PinAlgorithm = "sha256-spki"
)

type Root struct {
	Cert   *x509.Certificate
	Signer ed25519.PrivateKey
}

type Issued struct {
	Certificate *x509.Certificate
	PrivateKey  ed25519.PrivateKey
	CertPEM     []byte
	KeyPEM      []byte
}

func CreateRoot(commonName string, years int) (*Root, []byte, ed25519.PrivateKey, error) {
	if commonName == "" {
		commonName = "AlceaMesh Root CA"
	}
	if years <= 0 {
		years = RootYears
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("pki: generate root key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, nil, nil, err
	}

	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: commonName, Organization: []string{"Alcea"}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(years, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("pki: create root certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("pki: parse root certificate: %w", err)
	}
	return &Root{Cert: cert, Signer: private}, EncodeCertificate(cert), private, nil
}

func LoadRoot(certPEM, keyPEM []byte, passphrase string) (*Root, error) {
	cert, err := ParseCertificate(certPEM)
	if err != nil {
		return nil, err
	}
	if !cert.IsCA {
		return nil, errors.New("pki: this certificate is not a CA")
	}
	key, err := DecodePrivateKey(keyPEM, passphrase)
	if err != nil {
		return nil, err
	}
	public, ok := cert.PublicKey.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("pki: the root key must be ed25519, got %T", cert.PublicKey)
	}
	if !public.Equal(key.Public()) {
		return nil, errors.New("pki: the root certificate and key do not match")
	}
	return &Root{Cert: cert, Signer: key}, nil
}

type ServerOptions struct {
	DNSNames []string
	IPs      []net.IP
	Years    int
}

func (r *Root) IssueServer(options ServerOptions) (*Issued, error) {
	if len(options.DNSNames) == 0 && len(options.IPs) == 0 {
		return nil, errors.New("pki: a server certificate needs at least one DNS name or IP")
	}
	years := options.Years
	if years <= 0 {
		years = ServerYears
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("pki: generate leaf key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: firstCommonName(options), Organization: []string{"Alcea"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(years, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     options.DNSNames,
		IPAddresses:  options.IPs,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, r.Cert, public, r.Signer)
	if err != nil {
		return nil, fmt.Errorf("pki: create server certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("pki: parse server certificate: %w", err)
	}
	keyPEM, err := EncodePrivateKey(private, "")
	if err != nil {
		return nil, err
	}
	return &Issued{
		Certificate: cert,
		PrivateKey:  private,
		CertPEM:     EncodeCertificate(cert),
		KeyPEM:      keyPEM,
	}, nil
}

func firstCommonName(options ServerOptions) string {
	if len(options.DNSNames) > 0 {
		return options.DNSNames[0]
	}
	return options.IPs[0].String()
}

func Pin(cert *x509.Certificate) (string, error) {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:]), nil
}

func PinHex(cert *x509.Certificate) (string, error) {
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(sum[:]), nil
}

type VerifyOptions struct {
	Root       []byte
	ServerName string
	At         time.Time
}

func ValidateServer(leafPEM []byte, options VerifyOptions) (*x509.Certificate, error) {
	leaf, err := ParseCertificate(leafPEM)
	if err != nil {
		return nil, err
	}
	root, err := ParseCertificate(options.Root)
	if err != nil {
		return nil, err
	}

	roots := x509.NewCertPool()
	roots.AddCert(root)
	at := options.At
	if at.IsZero() {
		at = time.Now()
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		DNSName:       options.ServerName,
		CurrentTime:   at,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		Intermediates: x509.NewCertPool(),
	}); err != nil {
		return nil, fmt.Errorf("pki: the server certificate is not valid for %q: %w", options.ServerName, err)
	}
	return leaf, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("pki: serial: %w", err)
	}
	return serial, nil
}
