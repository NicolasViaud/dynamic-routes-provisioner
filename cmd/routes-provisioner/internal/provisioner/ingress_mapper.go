package provisioner

import (
	"fmt"
	"strings"

	core "github.com/NicolasViaud/dynamic-route-provisioner/core"
	provingress "github.com/NicolasViaud/dynamic-route-provisioner/provisioner-ingress"
	networkingv1 "k8s.io/api/networking/v1"
)

// DefaultSecretName derives a TLS Secret name from a hostname using a simple
// convention: tls-<host-with-dashes>. This is used as a fallback when
// certstore-kube is not enabled (e.g. Secrets managed by cert-manager or a
// cluster admin).
func DefaultSecretName(host string) string {
	safe := strings.NewReplacer(".", "-", "*", "wildcard").Replace(host)
	name := "tls-" + safe
	if len(name) > 253 {
		name = name[:253]
	}
	return name
}

// Compile-time check.
var _ provingress.IngressMapper = (*IngressMapper)(nil)

// IngressMapper translates RouteRequests into Kubernetes Ingress resources
// for the routes-provisioner application.
type IngressMapper struct {
	// SecretNameFunc derives the K8s Secret name for a given hostname.
	// Typically injected from certstore-kube via SecretNameFunc(), but can
	// also be a static function for admin-managed Secrets.
	// Must not be nil.
	SecretNameFunc   func(host string) string
	IngressClassName string // e.g. "nginx", empty for cluster default
}

// RouteID derives a stable route identifier from the request host.
func (m *IngressMapper) RouteID(req core.RouteRequest) string {
	return "routes-" + strings.ReplaceAll(req.Host, ".", "-")
}

// ExtractRouteID reconstructs the RouteID from an IngressRule.
func (m *IngressMapper) ExtractRouteID(rule networkingv1.IngressRule) string {
	return "routes-" + strings.ReplaceAll(rule.Host, ".", "-")
}

// MapRule converts a RouteRequest into an IngressRule with one HTTP path per backend.
func (m *IngressMapper) MapRule(req core.RouteRequest) (networkingv1.IngressRule, error) {
	if len(req.Backends) == 0 {
		return networkingv1.IngressRule{}, fmt.Errorf("route %s has no backends", req.ID)
	}

	pathType := networkingv1.PathTypePrefix
	path := req.Path
	if path == "" {
		path = "/"
	}

	backend := req.Backends[0]
	return networkingv1.IngressRule{
		Host: req.Host,
		IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{
					{
						Path:     path,
						PathType: &pathType,
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: backend.ServiceName,
								Port: networkingv1.ServiceBackendPort{
									Number: int32(backend.Port),
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

// MapTLS returns the IngressTLS entry for a route. The SecretNameFunc is
// called to derive the Secret name — it must be set at construction time.
func (m *IngressMapper) MapTLS(req core.RouteRequest) (*networkingv1.IngressTLS, error) {
	return &networkingv1.IngressTLS{
		Hosts:      []string{req.Host},
		SecretName: m.SecretNameFunc(req.Host),
	}, nil
}

// IngressName generates a name for the Ingress bucket at the given index.
func (m *IngressMapper) IngressName(index int) string {
	return fmt.Sprintf("routes-%d", index)
}

// Annotations returns annotations for managed Ingress resources.
func (m *IngressMapper) Annotations() map[string]string {
	return nil
}

// Labels returns labels for managed Ingress resources.
func (m *IngressMapper) Labels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "dynamic-route-provisioner",
	}
}

// IngressClass returns the IngressClassName.
func (m *IngressMapper) IngressClass() string {
	return m.IngressClassName
}
