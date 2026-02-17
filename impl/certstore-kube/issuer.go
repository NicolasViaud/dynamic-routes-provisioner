package certstorekube

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	core "github.com/NicolasViaud/dynamic-route-provisioner/core"
	"github.com/NicolasViaud/dynamic-route-provisioner/core/certificate"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Compile-time check.
var _ certificate.Issuer = (*CachingIssuer)(nil)

const (
	annotationHost      = "route-provisioner/host"
	annotationIssuer    = "route-provisioner/issuer"
	annotationNotBefore = "route-provisioner/not-before"
	annotationNotAfter  = "route-provisioner/not-after"

	dataKeyCA = "ca.crt"
)

// CachingIssuer wraps a certificate.Issuer and caches issued certificates as
// Kubernetes TLS Secrets. On subsequent Issue calls for the same host, it
// returns the cached certificate if it is still valid (not near expiry).
type CachingIssuer struct {
	inner     certificate.Issuer
	clientset kubernetes.Interface
	cfg       config
	logger    *slog.Logger
}

// New creates a CachingIssuer that delegates to inner and stores results in
// Kubernetes Secrets.
func New(inner certificate.Issuer, clientset kubernetes.Interface, logger *slog.Logger, opts ...Option) *CachingIssuer {
	if logger == nil {
		logger = slog.Default()
	}
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	return &CachingIssuer{
		inner:     inner,
		clientset: clientset,
		cfg:       cfg,
		logger:    logger,
	}
}

// Name returns the inner issuer's name — the decorator is transparent.
func (c *CachingIssuer) Name() string { return c.inner.Name() }

// Issue returns a cached certificate from a Kubernetes Secret if one exists and
// is still valid. Otherwise it delegates to the inner issuer and stores the
// result.
func (c *CachingIssuer) Issue(ctx context.Context, req core.RouteRequest) (*core.Certificate, error) {
	name := c.secretName(req.Host)
	log := c.logger.With("host", req.Host, "secret", name)

	secret, err := c.clientset.CoreV1().Secrets(c.cfg.namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		cert := c.secretToCert(secret)
		if cert != nil && time.Until(cert.NotAfter) > c.cfg.renewBefore {
			log.Info("certificate cache hit", "expires", cert.NotAfter)
			return cert, nil
		}
		log.Info("cached certificate expired or near expiry, re-issuing")
	} else if !k8serr.IsNotFound(err) {
		return nil, fmt.Errorf("get secret %s: %w", name, err)
	}

	cert, err := c.inner.Issue(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := c.storeCert(ctx, name, cert); err != nil {
		log.Error("failed to store certificate in secret", "error", err)
	} else {
		log.Info("certificate stored in secret", "expires", cert.NotAfter)
	}

	return cert, nil
}

// Revoke deletes the cached Secret and delegates to the inner issuer.
func (c *CachingIssuer) Revoke(ctx context.Context, cert core.Certificate) error {
	name := c.secretName(cert.Host)

	err := c.clientset.CoreV1().Secrets(c.cfg.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !k8serr.IsNotFound(err) {
		c.logger.Error("failed to delete certificate secret", "secret", name, "error", err)
	}

	return c.inner.Revoke(ctx, cert)
}

// secretName converts a hostname into a valid Kubernetes Secret name.
func (c *CachingIssuer) secretName(host string) string {
	safe := strings.NewReplacer(".", "-", "*", "wildcard").Replace(host)
	name := c.cfg.secretPrefix + "-" + safe
	if len(name) > 253 {
		name = name[:253]
	}
	return name
}

// secretToCert parses a Kubernetes TLS Secret back into a core.Certificate.
func (c *CachingIssuer) secretToCert(secret *corev1.Secret) *core.Certificate {
	certPEM := secret.Data[corev1.TLSCertKey]
	keyPEM := secret.Data[corev1.TLSPrivateKeyKey]
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		return nil
	}

	cert := &core.Certificate{
		Host:    secret.Annotations[annotationHost],
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	}

	if ca, ok := secret.Data[dataKeyCA]; ok {
		cert.CACertPEM = ca
	}

	cert.IssuerName = secret.Annotations[annotationIssuer]

	if v, ok := secret.Annotations[annotationNotBefore]; ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			cert.NotBefore = t
		}
	}
	if v, ok := secret.Annotations[annotationNotAfter]; ok {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			cert.NotAfter = t
		}
	}

	return cert
}

// storeCert creates or updates a Kubernetes TLS Secret with the certificate data.
func (c *CachingIssuer) storeCert(ctx context.Context, name string, cert *core.Certificate) error {
	annotations := map[string]string{
		annotationHost:      cert.Host,
		annotationIssuer:    cert.IssuerName,
		annotationNotBefore: cert.NotBefore.UTC().Format(time.RFC3339),
		annotationNotAfter:  cert.NotAfter.UTC().Format(time.RFC3339),
	}

	data := map[string][]byte{
		corev1.TLSCertKey:       cert.CertPEM,
		corev1.TLSPrivateKeyKey: cert.KeyPEM,
	}
	if len(cert.CACertPEM) > 0 {
		data[dataKeyCA] = cert.CACertPEM
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   c.cfg.namespace,
			Labels:      c.cfg.labels,
			Annotations: annotations,
		},
		Type: corev1.SecretTypeTLS,
		Data: data,
	}

	secrets := c.clientset.CoreV1().Secrets(c.cfg.namespace)

	_, err := secrets.Get(ctx, name, metav1.GetOptions{})
	if k8serr.IsNotFound(err) {
		_, err = secrets.Create(ctx, secret, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}

	_, err = secrets.Update(ctx, secret, metav1.UpdateOptions{})
	return err
}
