package provisioner

import (
	"context"

	core "github.com/NicolasViaud/dynamic-route-provisioner/core"
)

// RouteProvisioner creates, updates, or deletes routes on a target gateway/proxy.
// Implementations: Netscaler CPX (Nitro API), Envoy xDS, Nginx, Kubernetes Gateway API, etc.
type RouteProvisioner interface {
	// Provision creates or updates a route on the target gateway using the
	// provided certificate.
	Provision(ctx context.Context, req core.RouteRequest, cert *core.Certificate) (*core.ProvisionedRoute, error)

	// Deprovision removes a route from the target gateway.
	Deprovision(ctx context.Context, routeID string) error

	// List returns all routes currently provisioned on the gateway.
	List(ctx context.Context) ([]core.ProvisionedRoute, error)

	// BatchProvision applies multiple routes in a single batch operation.
	// certs is keyed by route host.
	BatchProvision(ctx context.Context, routes []core.RouteRequest, certs map[string]*core.Certificate) ([]core.ProvisionedRoute, error)

	// BatchDeprovision removes multiple routes in a single batch operation.
	BatchDeprovision(ctx context.Context, routeIDs []string) error

	// Name returns the identifier of this provisioner implementation.
	Name() string
}
