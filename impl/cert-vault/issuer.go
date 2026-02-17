package certvault

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"

	core "github.com/NicolasViaud/dynamic-route-provisioner/core"
	"github.com/NicolasViaud/dynamic-route-provisioner/core/certificate"

	vault "github.com/hashicorp/vault/api"
)

// Compile-time check.
var _ certificate.Issuer = (*VaultIssuer)(nil)

// VaultIssuer obtains TLS certificates from HashiCorp Vault's PKI secrets
// engine using role-based issuance.
type VaultIssuer struct {
	client *vault.Client
	cfg    config
}

// New creates a VaultIssuer. The Vault client is configured from options;
// VAULT_ADDR and VAULT_TOKEN environment variables are used as fallbacks.
func New(opts ...Option) (*VaultIssuer, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}

	vaultCfg := vault.DefaultConfig()
	if cfg.address != "" {
		vaultCfg.Address = cfg.address
	}

	client, err := vault.NewClient(vaultCfg)
	if err != nil {
		return nil, fmt.Errorf("create vault client: %w", err)
	}

	if cfg.token != "" {
		client.SetToken(cfg.token)
	}

	return &VaultIssuer{
		client: client,
		cfg:    cfg,
	}, nil
}

// Name returns "vault-pki".
func (v *VaultIssuer) Name() string { return "vault-pki" }

// Issue requests a certificate from Vault's PKI secrets engine for the host
// in req.
func (v *VaultIssuer) Issue(ctx context.Context, req core.RouteRequest) (*core.Certificate, error) {
	path := fmt.Sprintf("%s/issue/%s", v.cfg.mount, v.cfg.role)

	secret, err := v.client.Logical().WriteWithContext(ctx, path, map[string]interface{}{
		"common_name": req.Host,
		"ttl":         v.cfg.ttl,
	})
	if err != nil {
		return nil, fmt.Errorf("vault pki issue: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("vault pki issue: empty response")
	}

	certPEM, _ := secret.Data["certificate"].(string)
	keyPEM, _ := secret.Data["private_key"].(string)
	if certPEM == "" || keyPEM == "" {
		return nil, fmt.Errorf("vault pki issue: missing certificate or private_key in response")
	}

	// Build CA chain PEM from ca_chain array.
	var caPEM []byte
	if chain, ok := secret.Data["ca_chain"].([]interface{}); ok {
		for _, c := range chain {
			if s, ok := c.(string); ok {
				caPEM = append(caPEM, []byte(s)...)
				caPEM = append(caPEM, '\n')
			}
		}
	}

	// Parse leaf certificate to extract NotBefore/NotAfter.
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, fmt.Errorf("vault pki issue: failed to decode certificate PEM")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("vault pki issue: parse certificate: %w", err)
	}

	return &core.Certificate{
		Host:       req.Host,
		CertPEM:    []byte(certPEM),
		KeyPEM:     []byte(keyPEM),
		CACertPEM:  caPEM,
		NotBefore:  leaf.NotBefore,
		NotAfter:   leaf.NotAfter,
		IssuerName: v.Name(),
	}, nil
}

// Revoke revokes a previously issued certificate via Vault's PKI engine.
func (v *VaultIssuer) Revoke(ctx context.Context, cert core.Certificate) error {
	block, _ := pem.Decode(cert.CertPEM)
	if block == nil {
		return fmt.Errorf("failed to decode certificate PEM")
	}
	parsed, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse certificate: %w", err)
	}

	// Vault expects colon-separated hex serial number.
	serial := formatSerial(parsed.SerialNumber.Bytes())

	path := fmt.Sprintf("%s/revoke", v.cfg.mount)
	_, err = v.client.Logical().WriteWithContext(ctx, path, map[string]interface{}{
		"serial_number": serial,
	})
	if err != nil {
		return fmt.Errorf("vault pki revoke: %w", err)
	}

	return nil
}

// formatSerial converts raw serial number bytes to colon-separated hex.
func formatSerial(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("%02x", v)
	}
	return strings.Join(parts, ":")
}
