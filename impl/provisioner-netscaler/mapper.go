package provnetscaler

import core "github.com/nicol/dynamic-route-provisioner/core"

// ResourceMapper is the abstraction that developers implement to define how
// a RouteRequest and Certificate translate into Nitro API operations.
//
// This gives full control over which Netscaler resources are created:
// vserver type (csvserver vs lbvserver), backend services/servicegroups,
// SSL bindings, content-switching policies, etc.
type ResourceMapper interface {
	// MapProvision returns an ordered sequence of Nitro operations to create
	// or update a route on the Netscaler. The provisioner executes them
	// sequentially in the returned order.
	MapProvision(req core.RouteRequest, cert *core.Certificate) ([]NitroOperation, error)

	// MapDeprovision returns an ordered sequence of Nitro operations to
	// remove a route from the Netscaler.
	MapDeprovision(routeID string) ([]NitroOperation, error)

	// RouteID derives a stable identifier for the provisioned route from the
	// request. This ID is used to track and deprovision the route later.
	RouteID(req core.RouteRequest) string
}
