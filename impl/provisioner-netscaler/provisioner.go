package provnetscaler

import (
	"context"
	"fmt"

	core "github.com/nicol/dynamic-route-provisioner/core"
	"github.com/nicol/dynamic-route-provisioner/core/provisioner"
)

// Compile-time check that NetscalerProvisioner satisfies provisioner.RouteProvisioner.
var _ provisioner.RouteProvisioner = (*NetscalerProvisioner)(nil)

// NetscalerProvisioner creates and removes routes on a Netscaler CPX by
// executing Nitro API operations defined by a developer-provided ResourceMapper.
type NetscalerProvisioner struct {
	client *NitroClient
	mapper ResourceMapper
}

// New creates a NetscalerProvisioner. The mapper defines how route requests
// translate to Nitro API resources; options configure the connection.
func New(mapper ResourceMapper, opts ...Option) *NetscalerProvisioner {
	cfg := config{}
	for _, o := range opts {
		o(&cfg)
	}

	client := NewNitroClient(cfg.endpoint, cfg.username, cfg.password, cfg.httpClient)

	return &NetscalerProvisioner{
		client: client,
		mapper: mapper,
	}
}

// Name returns "netscaler-cpx".
func (p *NetscalerProvisioner) Name() string { return "netscaler-cpx" }

// Provision creates or updates a route on the Netscaler by executing the
// Nitro operations returned by the ResourceMapper.
func (p *NetscalerProvisioner) Provision(ctx context.Context, req core.RouteRequest, cert *core.Certificate) (*core.ProvisionedRoute, error) {
	ops, err := p.mapper.MapProvision(req, cert)
	if err != nil {
		return nil, fmt.Errorf("map provision operations: %w", err)
	}

	for i, op := range ops {
		if err := p.client.Execute(ctx, op); err != nil {
			return nil, fmt.Errorf("operation %d (%s %s/%s) failed: %w",
				i, op.Action, op.Resource.Type, op.Resource.Name, err)
		}
	}

	routeID := p.mapper.RouteID(req)

	certID := ""
	if cert != nil {
		certID = cert.Host
	}

	return &core.ProvisionedRoute{
		RouteID:       routeID,
		Host:          req.Host,
		GatewayID:     p.client.endpoint,
		CertificateID: certID,
		Status:        "active",
		ProviderName:  p.Name(),
	}, nil
}

// Deprovision removes a route from the Netscaler by executing the cleanup
// operations returned by the ResourceMapper.
func (p *NetscalerProvisioner) Deprovision(ctx context.Context, routeID string) error {
	ops, err := p.mapper.MapDeprovision(routeID)
	if err != nil {
		return fmt.Errorf("map deprovision operations: %w", err)
	}

	for i, op := range ops {
		if err := p.client.Execute(ctx, op); err != nil {
			return fmt.Errorf("operation %d (%s %s/%s) failed: %w",
				i, op.Action, op.Resource.Type, op.Resource.Name, err)
		}
	}

	return nil
}

// List queries the Netscaler for all provisioned routes matching the mapper's
// resource type and name prefix.
func (p *NetscalerProvisioner) List(ctx context.Context) ([]core.ProvisionedRoute, error) {
	resourceType, namePrefix := p.mapper.MapList()

	resources, err := p.client.List(ctx, resourceType, "name:"+namePrefix)
	if err != nil {
		return nil, fmt.Errorf("list %s resources: %w", resourceType, err)
	}

	var routes []core.ProvisionedRoute
	for _, res := range resources {
		name, _ := res["name"].(string)
		routes = append(routes, core.ProvisionedRoute{
			RouteID:      name,
			GatewayID:    p.client.endpoint,
			Status:       "active",
			ProviderName: p.Name(),
		})
	}

	return routes, nil
}

// BatchProvision applies multiple routes in a single Nitro batch request.
func (p *NetscalerProvisioner) BatchProvision(ctx context.Context, routes []core.RouteRequest, certs map[string]*core.Certificate) ([]core.ProvisionedRoute, error) {
	ops, err := p.mapper.MapBatchProvision(routes, certs)
	if err != nil {
		return nil, fmt.Errorf("map batch provision operations: %w", err)
	}

	if err := p.client.BatchExecute(ctx, ops); err != nil {
		return nil, fmt.Errorf("batch execute failed: %w", err)
	}

	var results []core.ProvisionedRoute
	for _, req := range routes {
		certID := ""
		if c, ok := certs[req.Host]; ok && c != nil {
			certID = c.Host
		}
		results = append(results, core.ProvisionedRoute{
			RouteID:       p.mapper.RouteID(req),
			Host:          req.Host,
			GatewayID:     p.client.endpoint,
			CertificateID: certID,
			Status:        "active",
			ProviderName:  p.Name(),
		})
	}

	return results, nil
}

// BatchDeprovision removes multiple routes in a single Nitro batch request.
func (p *NetscalerProvisioner) BatchDeprovision(ctx context.Context, routeIDs []string) error {
	ops, err := p.mapper.MapBatchDeprovision(routeIDs)
	if err != nil {
		return fmt.Errorf("map batch deprovision operations: %w", err)
	}

	return p.client.BatchExecute(ctx, ops)
}
