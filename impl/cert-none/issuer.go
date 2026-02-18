package certnone

import (
	"context"

	core "github.com/NicolasViaud/dynamic-route-provisioner/core"
	"github.com/NicolasViaud/dynamic-route-provisioner/core/certificate"
)

// Compile-time check.
var _ certificate.Issuer = (*NoneIssuer)(nil)

// NoneIssuer is a no-op certificate issuer. It delegates certificate
// management entirely to an external system (e.g. cert-manager). Issue
// always returns nil, signalling to the provisioner that no certificate
// data is available from this component — the provisioner is expected to
// reference an externally managed TLS secret directly.
type NoneIssuer struct{}

// New creates a NoneIssuer.
func New() *NoneIssuer { return &NoneIssuer{} }

// Name returns "none".
func (i *NoneIssuer) Name() string { return "none" }

// Issue is a no-op. It returns nil, nil so the provisioner receives a nil
// certificate and can fall back to its own secret-name convention.
func (i *NoneIssuer) Issue(_ context.Context, _ core.RouteRequest) (*core.Certificate, error) {
	return nil, nil
}

// Revoke is a no-op.
func (i *NoneIssuer) Revoke(_ context.Context, _ core.Certificate) error { return nil }
