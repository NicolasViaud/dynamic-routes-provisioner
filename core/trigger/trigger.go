package trigger

import (
	"context"

	core "github.com/nicol/dynamic-route-provisioner/core"
)

// Trigger watches for route changes from an external source and emits events.
// Implementations: webhook receiver, MongoDB change stream, event broker consumer, polling, etc.
type Trigger interface {
	// Start begins watching for route events. It sends events to the provided
	// channel and blocks until the context is cancelled or a fatal error occurs.
	Start(ctx context.Context, events chan<- core.RouteEvent) error

	// Name returns the identifier of this trigger implementation.
	Name() string
}
