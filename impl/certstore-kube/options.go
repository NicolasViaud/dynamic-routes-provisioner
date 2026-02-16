package certstorekube

import "time"

// Option configures a CachingIssuer.
type Option func(*config)

type config struct {
	namespace    string
	secretPrefix string
	renewBefore  time.Duration
	labels       map[string]string
}

func defaultConfig() config {
	return config{
		namespace:    "default",
		secretPrefix: "route-tls",
		renewBefore:  30 * 24 * time.Hour,
		labels: map[string]string{
			"app.kubernetes.io/managed-by": "route-provisioner",
		},
	}
}

// WithNamespace sets the Kubernetes namespace for certificate Secrets.
func WithNamespace(ns string) Option {
	return func(c *config) {
		c.namespace = ns
	}
}

// WithSecretPrefix sets the prefix for Secret names (e.g. "route-tls" → "route-tls-example-com").
func WithSecretPrefix(prefix string) Option {
	return func(c *config) {
		c.secretPrefix = prefix
	}
}

// WithRenewBefore sets how long before expiry a cached certificate is considered
// stale and will be re-issued via the inner issuer.
func WithRenewBefore(d time.Duration) Option {
	return func(c *config) {
		c.renewBefore = d
	}
}

// WithLabels sets the labels applied to managed Secrets.
func WithLabels(labels map[string]string) Option {
	return func(c *config) {
		c.labels = labels
	}
}
