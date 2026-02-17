package certstorefile

import "time"

// Option configures a CachingIssuer.
type Option func(*config)

type config struct {
	renewBefore time.Duration
}

func defaultConfig() config {
	return config{
		renewBefore: 30 * 24 * time.Hour, // 30 days
	}
}

// WithRenewBefore sets how long before expiry a cached certificate is
// considered stale and re-issued.
func WithRenewBefore(d time.Duration) Option {
	return func(c *config) {
		c.renewBefore = d
	}
}
