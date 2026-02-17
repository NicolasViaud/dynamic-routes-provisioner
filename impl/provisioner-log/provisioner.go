package provlog

import (
	"context"
	"log/slog"
	"sync"

	core "github.com/nicol/dynamic-route-provisioner/core"
	"github.com/nicol/dynamic-route-provisioner/core/provisioner"
)

// Compile-time check.
var _ provisioner.RouteProvisioner = (*LogProvisioner)(nil)

// LogProvisioner implements provisioner.RouteProvisioner by logging every
// operation to the console. It keeps an in-memory list of provisioned routes
// so the reconciler can diff against it. Useful for testing and development.
type LogProvisioner struct {
	mu     sync.RWMutex
	routes map[string]core.ProvisionedRoute // keyed by routeID
	logger *slog.Logger
}

// New creates a LogProvisioner.
func New(logger *slog.Logger) *LogProvisioner {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogProvisioner{
		routes: make(map[string]core.ProvisionedRoute),
		logger: logger,
	}
}

// Name returns "log".
func (p *LogProvisioner) Name() string { return "log" }

// Provision logs the route and stores it in memory.
func (p *LogProvisioner) Provision(ctx context.Context, req core.RouteRequest, cert *core.Certificate) (*core.ProvisionedRoute, error) {
	routeID := req.Host
	if req.ID != "" {
		routeID = req.ID
	}

	certInfo := "none"
	if cert != nil {
		certInfo = cert.IssuerName + " (expires " + cert.NotAfter.Format("2006-01-02") + ")"
	}

	p.logger.Info("provisioning route",
		"route_id", routeID,
		"host", req.Host,
		"path", req.Path,
		"tls", req.TLS,
		"backends", len(req.Backends),
		"certificate", certInfo,
	)

	for i, b := range req.Backends {
		p.logger.Info("  backend",
			"index", i,
			"service", b.ServiceName,
			"port", b.Port,
			"weight", b.Weight,
		)
	}

	route := core.ProvisionedRoute{
		RouteID:       routeID,
		Host:          req.Host,
		GatewayID:     "log-console",
		CertificateID: certHost(cert),
		Status:        "active",
		ProviderName:  p.Name(),
	}

	p.mu.Lock()
	p.routes[routeID] = route
	p.mu.Unlock()

	return &route, nil
}

// Deprovision logs the removal and removes from memory.
func (p *LogProvisioner) Deprovision(ctx context.Context, routeID string) error {
	p.logger.Info("deprovisioning route", "route_id", routeID)

	p.mu.Lock()
	delete(p.routes, routeID)
	p.mu.Unlock()

	return nil
}

// List returns the in-memory provisioned routes.
func (p *LogProvisioner) List(ctx context.Context) ([]core.ProvisionedRoute, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	routes := make([]core.ProvisionedRoute, 0, len(p.routes))
	for _, r := range p.routes {
		routes = append(routes, r)
	}

	p.logger.Info("listing routes", "count", len(routes))
	return routes, nil
}

// BatchProvision logs and stores each route.
func (p *LogProvisioner) BatchProvision(ctx context.Context, routes []core.RouteRequest, certs map[string]*core.Certificate) ([]core.ProvisionedRoute, error) {
	p.logger.Info("batch provisioning routes", "count", len(routes))

	results := make([]core.ProvisionedRoute, 0, len(routes))
	for _, req := range routes {
		result, err := p.Provision(ctx, req, certs[req.Host])
		if err != nil {
			return nil, err
		}
		results = append(results, *result)
	}
	return results, nil
}

// BatchDeprovision logs and removes each route.
func (p *LogProvisioner) BatchDeprovision(ctx context.Context, routeIDs []string) error {
	p.logger.Info("batch deprovisioning routes", "count", len(routeIDs))

	for _, id := range routeIDs {
		if err := p.Deprovision(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func certHost(cert *core.Certificate) string {
	if cert == nil {
		return ""
	}
	return cert.Host
}
