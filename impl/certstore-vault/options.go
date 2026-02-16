package certstorevault

import "time"

// Option configures a CachingIssuer.
type Option func(*config)

type config struct {
	address     string
	token       string
	mount       string
	prefix      string
	renewBefore time.Duration
}

func defaultConfig() config {
	return config{
		address:     "http://127.0.0.1:8200",
		mount:       "secret",
		prefix:      "route-tls",
		renewBefore: 30 * 24 * time.Hour,
	}
}

// WithAddress sets the Vault server address.
// If not set, falls back to VAULT_ADDR env var, then default.
func WithAddress(addr string) Option {
	return func(c *config) {
		c.address = addr
	}
}

// WithToken sets the Vault authentication token.
// If not set, falls back to VAULT_TOKEN env var.
func WithToken(token string) Option {
	return func(c *config) {
		c.token = token
	}
}

// WithMount sets the KV v2 secrets engine mount path (default: "secret").
func WithMount(mount string) Option {
	return func(c *config) {
		c.mount = mount
	}
}

// WithPrefix sets the path prefix under the KV mount for stored certificates
// (default: "route-tls").
func WithPrefix(prefix string) Option {
	return func(c *config) {
		c.prefix = prefix
	}
}

// WithRenewBefore sets how long before expiry a cached certificate is
// considered stale and will be re-issued via the inner issuer.
func WithRenewBefore(d time.Duration) Option {
	return func(c *config) {
		c.renewBefore = d
	}
}
