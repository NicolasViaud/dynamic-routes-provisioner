package certificate

import (
	"context"

	core "github.com/nicol/dynamic-route-provisioner/core"
)

// Issuer obtains TLS certificates for a given host.
// Implementations: ACME HTTP-01, ACME DNS-01, static/self-signed, vault, etc.
type Issuer interface {
	// Issue requests a certificate for the given route request.
	// It returns the issued certificate or an error.
	Issue(ctx context.Context, req core.RouteRequest) (*core.Certificate, error)

	// Revoke revokes a previously issued certificate.
	Revoke(ctx context.Context, cert core.Certificate) error

	// Name returns the identifier of this issuer implementation.
	Name() string
}
