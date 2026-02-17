package certstorefile

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	core "github.com/nicol/dynamic-route-provisioner/core"
	"github.com/nicol/dynamic-route-provisioner/core/certificate"
)

// Compile-time check.
var _ certificate.Issuer = (*CachingIssuer)(nil)

// CachingIssuer wraps any certificate.Issuer and caches issued certificates
// on the local filesystem. Useful for development and testing without
// Kubernetes or Vault.
//
// Directory layout:
//
//	<dir>/
//	  app.example.com/
//	    cert.pem
//	    key.pem
//	    ca.pem        (if CA chain present)
//	    meta.json     (issuer, notBefore, notAfter)
type CachingIssuer struct {
	inner  certificate.Issuer
	dir    string
	cfg    config
	logger *slog.Logger
}

// New creates a CachingIssuer that stores certificates under dir.
// The directory is created if it does not exist.
func New(inner certificate.Issuer, dir string, logger *slog.Logger, opts ...Option) *CachingIssuer {
	if logger == nil {
		logger = slog.Default()
	}
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	return &CachingIssuer{
		inner:  inner,
		dir:    dir,
		cfg:    cfg,
		logger: logger,
	}
}

// Name returns the inner issuer's name.
func (c *CachingIssuer) Name() string { return c.inner.Name() }

// Issue returns a cached certificate if it exists and is not near expiry.
// Otherwise it delegates to the inner issuer and caches the result.
func (c *CachingIssuer) Issue(ctx context.Context, req core.RouteRequest) (*core.Certificate, error) {
	hostDir := c.hostDir(req.Host)

	// Try loading from cache.
	cached, err := c.load(hostDir)
	if err == nil && time.Until(cached.NotAfter) > c.cfg.renewBefore {
		c.logger.Info("using cached certificate",
			"host", req.Host,
			"expires", cached.NotAfter.Format("2006-01-02"),
		)
		return cached, nil
	}

	// Issue new certificate.
	cert, err := c.inner.Issue(ctx, req)
	if err != nil {
		return nil, err
	}

	// Cache to disk.
	if err := c.store(hostDir, cert); err != nil {
		c.logger.Warn("failed to cache certificate", "host", req.Host, "error", err)
		// Non-fatal — return the cert anyway.
	} else {
		c.logger.Info("certificate cached",
			"host", req.Host,
			"dir", hostDir,
			"expires", cert.NotAfter.Format("2006-01-02"),
		)
	}

	return cert, nil
}

// Revoke removes the cached certificate and delegates to the inner issuer.
func (c *CachingIssuer) Revoke(ctx context.Context, cert core.Certificate) error {
	hostDir := c.hostDir(cert.Host)
	if err := os.RemoveAll(hostDir); err != nil && !os.IsNotExist(err) {
		c.logger.Warn("failed to remove cached certificate", "host", cert.Host, "error", err)
	}
	return c.inner.Revoke(ctx, cert)
}

// hostDir returns the directory path for a given host.
func (c *CachingIssuer) hostDir(host string) string {
	// Sanitize host for filesystem safety.
	safe := strings.ReplaceAll(host, "*", "_wildcard_")
	return filepath.Join(c.dir, safe)
}

// certMeta is the JSON metadata stored alongside PEM files.
type certMeta struct {
	Host       string    `json:"host"`
	IssuerName string    `json:"issuer"`
	NotBefore  time.Time `json:"not_before"`
	NotAfter   time.Time `json:"not_after"`
}

func (c *CachingIssuer) store(dir string, cert *core.Certificate) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), cert.CertPEM, 0o600); err != nil {
		return fmt.Errorf("write cert.pem: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), cert.KeyPEM, 0o600); err != nil {
		return fmt.Errorf("write key.pem: %w", err)
	}
	if len(cert.CACertPEM) > 0 {
		if err := os.WriteFile(filepath.Join(dir, "ca.pem"), cert.CACertPEM, 0o600); err != nil {
			return fmt.Errorf("write ca.pem: %w", err)
		}
	}

	meta := certMeta{
		Host:       cert.Host,
		IssuerName: cert.IssuerName,
		NotBefore:  cert.NotBefore,
		NotAfter:   cert.NotAfter,
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), metaBytes, 0o600); err != nil {
		return fmt.Errorf("write meta.json: %w", err)
	}

	return nil
}

func (c *CachingIssuer) load(dir string) (*core.Certificate, error) {
	metaBytes, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return nil, err
	}
	var meta certMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, err
	}

	certPEM, err := os.ReadFile(filepath.Join(dir, "cert.pem"))
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, "key.pem"))
	if err != nil {
		return nil, err
	}

	var caPEM []byte
	if data, err := os.ReadFile(filepath.Join(dir, "ca.pem")); err == nil {
		caPEM = data
	}

	return &core.Certificate{
		Host:       meta.Host,
		CertPEM:    certPEM,
		KeyPEM:     keyPEM,
		CACertPEM:  caPEM,
		NotBefore:  meta.NotBefore,
		NotAfter:   meta.NotAfter,
		IssuerName: meta.IssuerName,
	}, nil
}
