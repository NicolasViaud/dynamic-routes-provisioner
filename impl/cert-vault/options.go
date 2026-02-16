package certvault

// Option configures a VaultIssuer.
type Option func(*config)

type config struct {
	address string
	token   string
	mount   string
	role    string
	ttl     string
}

func defaultConfig() config {
	return config{
		address: "http://127.0.0.1:8200",
		mount:   "pki",
		ttl:     "720h",
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

// WithMount sets the PKI secrets engine mount path (default: "pki").
func WithMount(mount string) Option {
	return func(c *config) {
		c.mount = mount
	}
}

// WithRole sets the PKI role name used for certificate issuance.
func WithRole(role string) Option {
	return func(c *config) {
		c.role = role
	}
}

// WithTTL sets the requested certificate TTL (default: "720h").
func WithTTL(ttl string) Option {
	return func(c *config) {
		c.ttl = ttl
	}
}
