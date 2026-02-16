package leasemongo

import "time"

// Option configures a MongoLeaderElector.
type Option func(*config)

type config struct {
	leaseDuration time.Duration
	renewInterval time.Duration
	retryInterval time.Duration
	identity      string
	leaseName     string
}

func defaultConfig() config {
	return config{
		leaseDuration: 15 * time.Second,
		renewInterval: 5 * time.Second,
		retryInterval: 5 * time.Second,
		leaseName:     "leader",
	}
}

// WithLeaseDuration sets the duration the lease is valid before expiring.
func WithLeaseDuration(d time.Duration) Option {
	return func(c *config) {
		c.leaseDuration = d
	}
}

// WithRenewInterval sets how often the leader renews its lease.
func WithRenewInterval(d time.Duration) Option {
	return func(c *config) {
		c.renewInterval = d
	}
}

// WithRetryInterval sets how often a non-leader retries acquiring the lease.
func WithRetryInterval(d time.Duration) Option {
	return func(c *config) {
		c.retryInterval = d
	}
}

// WithIdentity sets the unique identity of this instance (e.g. hostname).
func WithIdentity(id string) Option {
	return func(c *config) {
		c.identity = id
	}
}

// WithLeaseName sets the name used as the document _id for the lease.
func WithLeaseName(name string) Option {
	return func(c *config) {
		c.leaseName = name
	}
}
