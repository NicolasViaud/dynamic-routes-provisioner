package certacmehttp

import "crypto"

const defaultDirectoryURL = "https://acme-v02.api.letsencrypt.org/directory"

// Option configures an ACMEIssuer.
type Option func(*config)

type config struct {
	directoryURL string
	email        string
	accountKey   crypto.Signer
}

func defaultConfig() config {
	return config{
		directoryURL: defaultDirectoryURL,
	}
}

// WithDirectoryURL sets the ACME directory URL.
// Defaults to Let's Encrypt production.
func WithDirectoryURL(url string) Option {
	return func(c *config) {
		c.directoryURL = url
	}
}

// WithEmail sets the contact email used during ACME account registration.
func WithEmail(email string) Option {
	return func(c *config) {
		c.email = email
	}
}

// WithAccountKey sets the ACME account private key. If not provided,
// a new ECDSA P-256 key is generated automatically.
func WithAccountKey(key crypto.Signer) Option {
	return func(c *config) {
		c.accountKey = key
	}
}
