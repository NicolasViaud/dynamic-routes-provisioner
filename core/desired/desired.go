package desired

import (
	"context"

	core "github.com/NicolasViaud/dynamic-route-provisioner/core"
)

// DesiredStateProvider reads the full desired state from the source of truth.
// Implementations: MongoDB collection scan, API call, file, etc.
type DesiredStateProvider interface {
	// List returns all routes that should currently exist.
	List(ctx context.Context) ([]core.RouteRequest, error)

	// Name returns the identifier of this provider.
	Name() string
}
