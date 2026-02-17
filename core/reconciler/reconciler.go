package reconciler

import (
	"context"
	"log/slog"

	core "github.com/NicolasViaud/dynamic-route-provisioner/core"
	"github.com/NicolasViaud/dynamic-route-provisioner/core/certificate"
	"github.com/NicolasViaud/dynamic-route-provisioner/core/desired"
	"github.com/NicolasViaud/dynamic-route-provisioner/core/provisioner"
)

// Reconciler compares the desired state (source of truth) against the actual
// state (gateway) and applies the diff using batch operations.
type Reconciler struct {
	desired     desired.DesiredStateProvider
	issuer      certificate.Issuer
	provisioner provisioner.RouteProvisioner
	logger      *slog.Logger
}

func New(d desired.DesiredStateProvider, i certificate.Issuer, p provisioner.RouteProvisioner, logger *slog.Logger) *Reconciler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Reconciler{
		desired:     d,
		issuer:      i,
		provisioner: p,
		logger:      logger,
	}
}

// Reconcile runs a full reconciliation cycle: list desired, list actual,
// compute diff, issue certs, batch-apply.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	r.logger.Info("starting reconciliation")

	// 1. Get desired state.
	desiredRoutes, err := r.desired.List(ctx)
	if err != nil {
		return err
	}

	// 2. Get actual state.
	actualRoutes, err := r.provisioner.List(ctx)
	if err != nil {
		return err
	}

	// 3. Compute diff.
	toAdd, toRemove := diff(desiredRoutes, actualRoutes)

	r.logger.Info("diff computed",
		"desired", len(desiredRoutes),
		"actual", len(actualRoutes),
		"to_add", len(toAdd),
		"to_remove", len(toRemove),
	)

	// 4. Deprovision routes that should not exist.
	if len(toRemove) > 0 {
		r.logger.Info("deprovisioning routes", "count", len(toRemove))
		if err := r.provisioner.BatchDeprovision(ctx, toRemove); err != nil {
			r.logger.Error("route deprovisioning failed", "error", err)
		}
	}

	// 5. Issue certificates for routes that need TLS.
	certs := make(map[string]*core.Certificate)
	for _, route := range toAdd {
		if !route.TLS {
			continue
		}
		r.logger.Info("issuing certificate", "host", route.Host)
		cert, err := r.issuer.Issue(ctx, route)
		if err != nil {
			r.logger.Error("certificate issuance failed",
				"host", route.Host, "error", err)
			continue
		}
		r.logger.Info("certificate issued",
			"host", route.Host, "issuer", cert.IssuerName, "expires", cert.NotAfter)
		certs[route.Host] = cert
	}

	// 6. Batch provision routes that should exist.
	if len(toAdd) > 0 {
		results, err := r.provisioner.BatchProvision(ctx, toAdd, certs)
		if err != nil {
			r.logger.Error("batch provision failed", "error", err)
			return err
		}
		r.logger.Info("routes provisioned", "count", len(results))
	}

	r.logger.Info("reconciliation done")
	return nil
}

// diff compares desired routes against actual provisioned routes.
// Returns routes to add (in desired, not in actual) and route IDs to remove
// (in actual, not in desired).
func diff(desiredRoutes []core.RouteRequest, actualRoutes []core.ProvisionedRoute) (toAdd []core.RouteRequest, toRemove []string) {
	actualByHost := make(map[string]core.ProvisionedRoute, len(actualRoutes))
	for _, a := range actualRoutes {
		actualByHost[a.Host] = a
	}

	desiredHosts := make(map[string]struct{}, len(desiredRoutes))
	for _, d := range desiredRoutes {
		desiredHosts[d.Host] = struct{}{}
		if _, exists := actualByHost[d.Host]; !exists {
			toAdd = append(toAdd, d)
		}
	}

	for _, a := range actualRoutes {
		if _, exists := desiredHosts[a.Host]; !exists {
			toRemove = append(toRemove, a.RouteID)
		}
	}

	return toAdd, toRemove
}
