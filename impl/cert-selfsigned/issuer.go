package certselfsigned

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	core "github.com/NicolasViaud/dynamic-route-provisioner/core"
	"github.com/NicolasViaud/dynamic-route-provisioner/core/certificate"
)

// Compile-time check.
var _ certificate.Issuer = (*SelfSignedIssuer)(nil)

// SelfSignedIssuer generates self-signed TLS certificates. Intended for
// development and testing — not for production use.
type SelfSignedIssuer struct {
	cfg config
}

// New creates a SelfSignedIssuer.
func New(opts ...Option) *SelfSignedIssuer {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	return &SelfSignedIssuer{cfg: cfg}
}

// Name returns "self-signed".
func (i *SelfSignedIssuer) Name() string { return "self-signed" }

// Issue generates a self-signed certificate for the route's host.
func (i *SelfSignedIssuer) Issue(_ context.Context, req core.RouteRequest) (*core.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   req.Host,
			Organization: []string{i.cfg.organization},
		},
		DNSNames:              []string{req.Host},
		NotBefore:             now,
		NotAfter:              now.Add(i.cfg.validity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return &core.Certificate{
		Host:       req.Host,
		CertPEM:    certPEM,
		KeyPEM:     keyPEM,
		NotBefore:  now,
		NotAfter:   now.Add(i.cfg.validity),
		IssuerName: i.Name(),
	}, nil
}

// Revoke is a no-op for self-signed certificates.
func (i *SelfSignedIssuer) Revoke(_ context.Context, _ core.Certificate) error {
	return nil
}
