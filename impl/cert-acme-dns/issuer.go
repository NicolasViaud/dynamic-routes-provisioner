package certacmedns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"

	core "github.com/nicol/dynamic-route-provisioner/core"
	"github.com/nicol/dynamic-route-provisioner/core/certificate"
	"golang.org/x/crypto/acme"
)

// Compile-time check that ACMEDNSIssuer satisfies certificate.Issuer.
var _ certificate.Issuer = (*ACMEDNSIssuer)(nil)

// ACMEDNSIssuer obtains TLS certificates using the ACME DNS-01 challenge.
// The developer provides a DNSProvider that controls how the challenge
// TXT record is created and cleaned up.
type ACMEDNSIssuer struct {
	client   *acme.Client
	provider DNSProvider
	cfg      config
}

// New creates an ACMEDNSIssuer. If no account key is provided via options,
// a new ECDSA P-256 key is generated.
func New(provider DNSProvider, opts ...Option) (*ACMEDNSIssuer, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}

	if cfg.accountKey == nil {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generate account key: %w", err)
		}
		cfg.accountKey = key
	}

	client := &acme.Client{
		Key:          cfg.accountKey,
		DirectoryURL: cfg.directoryURL,
	}

	return &ACMEDNSIssuer{
		client:   client,
		provider: provider,
		cfg:      cfg,
	}, nil
}

// Name returns "acme-dns-01".
func (a *ACMEDNSIssuer) Name() string { return "acme-dns-01" }

// Issue obtains a certificate for the host in req using the ACME DNS-01 flow.
func (a *ACMEDNSIssuer) Issue(ctx context.Context, req core.RouteRequest) (*core.Certificate, error) {
	// 1. Register account (idempotent — returns existing if already registered).
	acct := &acme.Account{}
	if a.cfg.email != "" {
		acct.Contact = []string{"mailto:" + a.cfg.email}
	}
	if _, err := a.client.Register(ctx, acct, acme.AcceptTOS); err != nil {
		if err != acme.ErrAccountAlreadyExists {
			return nil, fmt.Errorf("register account: %w", err)
		}
	}

	domain := req.Host

	// 2. Create order.
	order, err := a.client.AuthorizeOrder(ctx, []acme.AuthzID{
		{Type: "dns", Value: domain},
	})
	if err != nil {
		return nil, fmt.Errorf("authorize order: %w", err)
	}

	// 3. Process each authorization.
	for _, authzURL := range order.AuthzURLs {
		if err := a.fulfillAuthorization(ctx, domain, authzURL); err != nil {
			return nil, err
		}
	}

	// 4. Wait for order to be ready.
	order, err = a.client.WaitOrder(ctx, order.URI)
	if err != nil {
		return nil, fmt.Errorf("wait order: %w", err)
	}

	// 5. Generate certificate key and CSR.
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate cert key: %w", err)
	}

	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: domain},
		DNSNames: []string{domain},
	}, certKey)
	if err != nil {
		return nil, fmt.Errorf("create CSR: %w", err)
	}

	// 6. Finalize order — get the certificate chain (DER-encoded).
	derChain, _, err := a.client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, fmt.Errorf("finalize order: %w", err)
	}

	// 7. Encode to PEM.
	certPEM, caPEM, err := encodeDERChain(derChain)
	if err != nil {
		return nil, err
	}

	keyDER, err := x509.MarshalECPrivateKey(certKey)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	// 8. Parse leaf for metadata.
	leaf, err := x509.ParseCertificate(derChain[0])
	if err != nil {
		return nil, fmt.Errorf("parse leaf certificate: %w", err)
	}

	return &core.Certificate{
		Host:       domain,
		CertPEM:    certPEM,
		KeyPEM:     keyPEM,
		CACertPEM:  caPEM,
		NotBefore:  leaf.NotBefore,
		NotAfter:   leaf.NotAfter,
		IssuerName: a.Name(),
	}, nil
}

// Revoke revokes a previously issued certificate via ACME.
func (a *ACMEDNSIssuer) Revoke(ctx context.Context, cert core.Certificate) error {
	block, _ := pem.Decode(cert.CertPEM)
	if block == nil {
		return fmt.Errorf("failed to decode certificate PEM")
	}
	return a.client.RevokeCert(ctx, nil, block.Bytes, acme.CRLReasonUnspecified)
}

// fulfillAuthorization finds the DNS-01 challenge in the given authorization
// and completes it using the DNSProvider.
func (a *ACMEDNSIssuer) fulfillAuthorization(ctx context.Context, domain, authzURL string) error {
	authz, err := a.client.GetAuthorization(ctx, authzURL)
	if err != nil {
		return fmt.Errorf("get authorization: %w", err)
	}

	// Already valid (e.g. reused authorization).
	if authz.Status == acme.StatusValid {
		return nil
	}

	// Find the DNS-01 challenge.
	var chal *acme.Challenge
	for _, c := range authz.Challenges {
		if c.Type == "dns-01" {
			chal = c
			break
		}
	}
	if chal == nil {
		return fmt.Errorf("no dns-01 challenge found for %s", domain)
	}

	// Compute the TXT record value.
	record, err := a.client.DNS01ChallengeRecord(chal.Token)
	if err != nil {
		return fmt.Errorf("compute challenge record: %w", err)
	}

	// Present the challenge via the DNS provider.
	if err := a.provider.Present(ctx, domain, record); err != nil {
		return fmt.Errorf("present challenge: %w", err)
	}
	defer a.provider.Cleanup(ctx, domain)

	// Tell the ACME server we're ready.
	if _, err := a.client.Accept(ctx, chal); err != nil {
		return fmt.Errorf("accept challenge: %w", err)
	}

	// Wait until the authorization is valid.
	if _, err := a.client.WaitAuthorization(ctx, authzURL); err != nil {
		return fmt.Errorf("wait authorization: %w", err)
	}

	return nil
}

// encodeDERChain converts a DER certificate chain into PEM. Returns the leaf
// certificate PEM and the concatenated CA chain PEM separately.
func encodeDERChain(derChain [][]byte) (certPEM, caPEM []byte, err error) {
	if len(derChain) == 0 {
		return nil, nil, fmt.Errorf("empty certificate chain")
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derChain[0]})

	for _, der := range derChain[1:] {
		caPEM = append(caPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}

	return certPEM, caPEM, nil
}
