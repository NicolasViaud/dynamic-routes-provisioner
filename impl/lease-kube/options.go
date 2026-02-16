package leasekube

import "time"

// Option configures a KubeLeaderElector.
type Option func(*config)

type config struct {
	namespace     string
	leaseName     string
	identity      string
	leaseDuration time.Duration
	renewDeadline time.Duration
	retryPeriod   time.Duration
}

func defaultConfig() config {
	return config{
		namespace:     "default",
		leaseName:     "leader",
		leaseDuration: 15 * time.Second,
		renewDeadline: 10 * time.Second,
		retryPeriod:   2 * time.Second,
	}
}

// WithNamespace sets the Kubernetes namespace for the Lease resource.
func WithNamespace(ns string) Option {
	return func(c *config) {
		c.namespace = ns
	}
}

// WithLeaseName sets the name of the Kubernetes Lease resource.
func WithLeaseName(name string) Option {
	return func(c *config) {
		c.leaseName = name
	}
}

// WithIdentity sets the unique identity of this instance.
func WithIdentity(id string) Option {
	return func(c *config) {
		c.identity = id
	}
}

// WithLeaseDuration sets how long a lease is valid before it expires.
func WithLeaseDuration(d time.Duration) Option {
	return func(c *config) {
		c.leaseDuration = d
	}
}

// WithRenewDeadline sets the deadline for the leader to renew the lease
// before it is considered lost.
func WithRenewDeadline(d time.Duration) Option {
	return func(c *config) {
		c.renewDeadline = d
	}
}

// WithRetryPeriod sets how often non-leaders retry acquiring the lease.
func WithRetryPeriod(d time.Duration) Option {
	return func(c *config) {
		c.retryPeriod = d
	}
}
