package lease

import "context"

// LeaderElector manages leader election using a distributed lease.
// Only one instance holds the lease at a time; the rest wait and attempt
// to acquire it when it expires or is released.
//
// Implementations: MongoDB document with TTL, Kubernetes coordination/v1
// Lease, etcd, etc.
type LeaderElector interface {
	// Run starts the leader election loop. It blocks until the context is
	// cancelled. The provided callbacks are invoked on leadership transitions.
	// OnStartedLeading is called when this instance becomes the leader;
	// its context is cancelled when leadership is lost.
	Run(ctx context.Context, callbacks LeaderCallbacks) error

	// IsLeader reports whether this instance currently holds the lease.
	IsLeader() bool

	// Name returns the identifier of this leader elector implementation.
	Name() string
}

// LeaderCallbacks contains functions invoked on leadership transitions.
type LeaderCallbacks struct {
	// OnStartedLeading is called when this instance becomes the leader.
	// The provided context is cancelled when leadership is lost.
	OnStartedLeading func(ctx context.Context)

	// OnStoppedLeading is called when this instance loses leadership.
	OnStoppedLeading func()
}
