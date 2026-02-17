package provingress

import (
	core "github.com/NicolasViaud/dynamic-route-provisioner/core"
	networkingv1 "k8s.io/api/networking/v1"
)

// IngressMapper is the abstraction that developers implement to define how
// RouteRequests translate into Kubernetes Ingress resources.
//
// This gives full control over Ingress rule construction, TLS secret naming,
// Ingress naming/labelling, and route identification. The provisioner calls
// these methods to build and manage packed Ingress resources.
type IngressMapper interface {
	// MapRule converts a RouteRequest into a networking/v1 IngressRule.
	// The rule should contain a single host and its associated HTTP paths.
	MapRule(req core.RouteRequest) (networkingv1.IngressRule, error)

	// MapTLS returns the IngressTLS entry for a TLS route. The SecretName
	// must match the naming convention used by the certificate store (e.g.
	// certstore-kube) so that the Ingress controller can find the Secret.
	MapTLS(req core.RouteRequest) (*networkingv1.IngressTLS, error)

	// IngressName generates a name for the Ingress "bucket" at the given
	// index. Example: "routes-0", "routes-1".
	IngressName(index int) string

	// Annotations returns annotations to set on managed Ingress resources.
	Annotations() map[string]string

	// Labels returns labels to set on managed Ingress resources. These MUST
	// include the management label used for label-selector queries.
	Labels() map[string]string

	// IngressClass returns the IngressClassName. Return an empty string to
	// use the cluster default.
	IngressClass() string

	// RouteID derives a stable identifier for the provisioned route from the
	// request. This ID is used to track and deprovision the route later.
	RouteID(req core.RouteRequest) string

	// ExtractRouteID reconstructs the RouteID from an IngressRule. This is
	// the reverse of RouteID and is used during List to rebuild
	// ProvisionedRoute entries from existing Ingress resources.
	ExtractRouteID(rule networkingv1.IngressRule) string
}
