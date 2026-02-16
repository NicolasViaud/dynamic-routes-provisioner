package provisioner

import (
	"context"

	core "github.com/nicol/dynamic-route-provisioner/core"
)

// RouteProvisioner creates, updates, or deletes routes on a target gateway/proxy.
// Implementations: Netscaler CPX (Nitro API), Envoy xDS, Nginx, Kubernetes Gateway API, etc.
type RouteProvisioner interface {
	// Provision creates or updates a route on the target gateway using the
	// provided certificate.
	Provision(ctx context.Context, req core.RouteRequest, cert *core.Certificate) (*core.ProvisionedRoute, error)

	// Deprovision removes a route from the target gateway.
	Deprovision(ctx context.Context, routeID string) error

	// Name returns the identifier of this provisioner implementation.
	Name() string
}
