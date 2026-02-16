package orchestrator

import (
	"context"
	"fmt"
	"log/slog"

	core "github.com/nicol/dynamic-route-provisioner/core"
	"github.com/nicol/dynamic-route-provisioner/core/certificate"
	"github.com/nicol/dynamic-route-provisioner/core/provisioner"
	"github.com/nicol/dynamic-route-provisioner/core/trigger"
)

// Orchestrator coordinates the route provisioning pipeline:
// Trigger → CertificateIssuer → RouteProvisioner.
type Orchestrator struct {
	trigger     trigger.Trigger
	issuer      certificate.Issuer
	provisioner provisioner.RouteProvisioner
	logger      *slog.Logger
}

func New(t trigger.Trigger, i certificate.Issuer, p provisioner.RouteProvisioner, logger *slog.Logger) *Orchestrator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Orchestrator{
		trigger:     t,
		issuer:      i,
		provisioner: p,
		logger:      logger,
	}
}

// Run starts the orchestrator. It listens for trigger events and processes them
// through the certificate issuance and route provisioning pipeline.
// It blocks until the context is cancelled.
func (o *Orchestrator) Run(ctx context.Context) error {
	events := make(chan core.RouteEvent)

	// Start the trigger in a separate goroutine.
	errCh := make(chan error, 1)
	go func() {
		errCh <- o.trigger.Start(ctx, events)
	}()

	o.logger.Info("orchestrator started",
		"trigger", o.trigger.Name(),
		"issuer", o.issuer.Name(),
		"provisioner", o.provisioner.Name(),
	)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-errCh:
			return fmt.Errorf("trigger %s failed: %w", o.trigger.Name(), err)

		case event := <-events:
			o.handleEvent(ctx, event)
		}
	}
}

func (o *Orchestrator) handleEvent(ctx context.Context, event core.RouteEvent) {
	log := o.logger.With("host", event.Route.Host, "event", event.Type)

	switch event.Type {
	case core.EventRouteCreated, core.EventRouteUpdated:
		o.provisionRoute(ctx, log, event.Route)
	case core.EventRouteDeleted:
		o.deprovisionRoute(ctx, log, event.Route)
	default:
		log.Warn("unknown event type, skipping")
	}
}

func (o *Orchestrator) provisionRoute(ctx context.Context, log *slog.Logger, route core.RouteRequest) {
	var cert *core.Certificate

	if route.TLS {
		log.Info("issuing certificate")
		var err error
		cert, err = o.issuer.Issue(ctx, route)
		if err != nil {
			log.Error("certificate issuance failed", "error", err)
			return
		}
		log.Info("certificate issued", "issuer", cert.IssuerName, "expires", cert.NotAfter)
	}

	log.Info("provisioning route")
	result, err := o.provisioner.Provision(ctx, route, cert)
	if err != nil {
		log.Error("route provisioning failed", "error", err)
		return
	}

	log.Info("route provisioned",
		"gateway", result.GatewayID,
		"provider", result.ProviderName,
		"status", result.Status,
	)
}

func (o *Orchestrator) deprovisionRoute(ctx context.Context, log *slog.Logger, route core.RouteRequest) {
	log.Info("deprovisioning route")
	if err := o.provisioner.Deprovision(ctx, route.ID); err != nil {
		log.Error("route deprovisioning failed", "error", err)
	}
}
