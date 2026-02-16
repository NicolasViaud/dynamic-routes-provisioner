package certselfsigned

import "time"

// Option configures a SelfSignedIssuer.
type Option func(*config)

type config struct {
	validity     time.Duration
	organization string
}

func defaultConfig() config {
	return config{
		validity:     365 * 24 * time.Hour,
		organization: "Self-Signed",
	}
}

// WithValidity sets how long the generated certificate is valid.
// Defaults to 1 year.
func WithValidity(d time.Duration) Option {
	return func(c *config) {
		c.validity = d
	}
}

// WithOrganization sets the organization name in the certificate subject.
func WithOrganization(org string) Option {
	return func(c *config) {
		c.organization = org
	}
}
