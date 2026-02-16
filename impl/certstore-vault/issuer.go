package certstorevault

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	core "github.com/nicol/dynamic-route-provisioner/core"
	"github.com/nicol/dynamic-route-provisioner/core/certificate"

	vault "github.com/hashicorp/vault/api"
)

// Compile-time check.
var _ certificate.Issuer = (*CachingIssuer)(nil)

// CachingIssuer wraps a certificate.Issuer and caches issued certificates in
// HashiCorp Vault's KV v2 secrets engine. On subsequent Issue calls for the
// same host, it returns the cached certificate if it is still valid.
type CachingIssuer struct {
	inner  certificate.Issuer
	client *vault.Client
	cfg    config
	logger *slog.Logger
}

// New creates a CachingIssuer that delegates to inner and stores results in
// Vault KV v2.
func New(inner certificate.Issuer, logger *slog.Logger, opts ...Option) (*CachingIssuer, error) {
	if logger == nil {
		logger = slog.Default()
	}
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

	return &CachingIssuer{
		inner:  inner,
		client: client,
		cfg:    cfg,
		logger: logger,
	}, nil
}

// Name returns the inner issuer's name — the decorator is transparent.
func (c *CachingIssuer) Name() string { return c.inner.Name() }

// Issue returns a cached certificate from Vault KV v2 if one exists and is
// still valid. Otherwise it delegates to the inner issuer and stores the result.
func (c *CachingIssuer) Issue(ctx context.Context, req core.RouteRequest) (*core.Certificate, error) {
	path := c.secretPath(req.Host)
	log := c.logger.With("host", req.Host, "path", path)

	secret, err := c.client.Logical().ReadWithContext(ctx, path)
	if err == nil && secret != nil && secret.Data != nil {
		cert := c.secretToCert(secret)
		if cert != nil && time.Until(cert.NotAfter) > c.cfg.renewBefore {
			log.Info("certificate cache hit", "expires", cert.NotAfter)
			return cert, nil
		}
		log.Info("cached certificate expired or near expiry, re-issuing")
	} else if err != nil {
		log.Warn("failed to read cached certificate", "error", err)
	}

	cert, err := c.inner.Issue(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := c.storeCert(ctx, req.Host, cert); err != nil {
		log.Error("failed to store certificate in vault", "error", err)
	} else {
		log.Info("certificate stored in vault", "expires", cert.NotAfter)
	}

	return cert, nil
}

// Revoke deletes the cached secret and delegates to the inner issuer.
func (c *CachingIssuer) Revoke(ctx context.Context, cert core.Certificate) error {
	// Delete all versions and metadata via the metadata path.
	metaPath := fmt.Sprintf("%s/metadata/%s/%s", c.cfg.mount, c.cfg.prefix, sanitizeHost(cert.Host))

	_, err := c.client.Logical().DeleteWithContext(ctx, metaPath)
	if err != nil {
		c.logger.Error("failed to delete certificate from vault", "path", metaPath, "error", err)
	}

	return c.inner.Revoke(ctx, cert)
}

// secretPath returns the KV v2 data path for a host.
func (c *CachingIssuer) secretPath(host string) string {
	return fmt.Sprintf("%s/data/%s/%s", c.cfg.mount, c.cfg.prefix, sanitizeHost(host))
}

// secretToCert parses a Vault KV v2 secret into a core.Certificate.
// KV v2 wraps actual data inside .Data["data"].
func (c *CachingIssuer) secretToCert(secret *vault.Secret) *core.Certificate {
	data, ok := secret.Data["data"].(map[string]interface{})
	if !ok || data == nil {
		return nil
	}

	certPEM, _ := data["cert_pem"].(string)
	keyPEM, _ := data["key_pem"].(string)
	if certPEM == "" || keyPEM == "" {
		return nil
	}

	cert := &core.Certificate{
		CertPEM: []byte(certPEM),
		KeyPEM:  []byte(keyPEM),
	}

	if host, ok := data["host"].(string); ok {
		cert.Host = host
	}
	if issuer, ok := data["issuer"].(string); ok {
		cert.IssuerName = issuer
	}
	if caPEM, ok := data["ca_pem"].(string); ok && caPEM != "" {
		cert.CACertPEM = []byte(caPEM)
	}
	if v, ok := data["not_before"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			cert.NotBefore = t
		}
	}
	if v, ok := data["not_after"].(string); ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			cert.NotAfter = t
		}
	}

	return cert
}

// storeCert writes a certificate to Vault KV v2.
func (c *CachingIssuer) storeCert(ctx context.Context, host string, cert *core.Certificate) error {
	path := c.secretPath(host)

	data := map[string]interface{}{
		"data": map[string]interface{}{
			"host":       cert.Host,
			"issuer":     cert.IssuerName,
			"cert_pem":   string(cert.CertPEM),
			"key_pem":    string(cert.KeyPEM),
			"ca_pem":     string(cert.CACertPEM),
			"not_before": cert.NotBefore.UTC().Format(time.RFC3339),
			"not_after":  cert.NotAfter.UTC().Format(time.RFC3339),
		},
	}

	_, err := c.client.Logical().WriteWithContext(ctx, path, data)
	return err
}

// sanitizeHost converts a hostname into a safe path segment.
func sanitizeHost(host string) string {
	return strings.NewReplacer(".", "-", "*", "wildcard").Replace(host)
}
