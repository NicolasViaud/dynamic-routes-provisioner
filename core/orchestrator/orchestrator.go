package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	core "github.com/nicol/dynamic-route-provisioner/core"
	"github.com/nicol/dynamic-route-provisioner/core/certificate"
	"github.com/nicol/dynamic-route-provisioner/core/lease"
	"github.com/nicol/dynamic-route-provisioner/core/provisioner"
	"github.com/nicol/dynamic-route-provisioner/core/reconciler"
	"github.com/nicol/dynamic-route-provisioner/core/trigger"
)

// Orchestrator coordinates the route provisioning pipeline. It runs two
// concurrent loops:
//   - Event-driven: reacts to trigger events in real-time
//   - Reconciliation: periodically compares desired vs actual state and fixes drift
type Orchestrator struct {
	trigger           trigger.Trigger
	issuer            certificate.Issuer
	provisioner       provisioner.RouteProvisioner
	reconciler        *reconciler.Reconciler
	reconcileInterval time.Duration
	leaderElector     lease.LeaderElector
	logger            *slog.Logger
	mu                sync.Mutex // serializes event handling and reconciliation
}

// Option configures an Orchestrator.
type Option func(*Orchestrator)

// WithReconciler enables state reconciliation with the given reconciler and interval.
// An interval of 0 means reconciliation runs only at startup.
func WithReconciler(r *reconciler.Reconciler, interval time.Duration) Option {
	return func(o *Orchestrator) {
		o.reconciler = r
		o.reconcileInterval = interval
	}
}

// WithLeaderElection enables leader election. When set, the orchestrator
// only processes events and runs reconciliation while this instance is the
// leader. On leadership loss the event loop and reconciliation stop; on
// re-election they restart automatically.
func WithLeaderElection(le lease.LeaderElector) Option {
	return func(o *Orchestrator) {
		o.leaderElector = le
	}
}

func New(t trigger.Trigger, i certificate.Issuer, p provisioner.RouteProvisioner, logger *slog.Logger, opts ...Option) *Orchestrator {
	if logger == nil {
		logger = slog.Default()
	}
	o := &Orchestrator{
		trigger:     t,
		issuer:      i,
		provisioner: p,
		logger:      logger,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Run starts the orchestrator. If leader election is configured, it blocks
// until leadership is acquired before starting the event loop and
// reconciliation. It blocks until the context is cancelled.
func (o *Orchestrator) Run(ctx context.Context) error {
	if o.leaderElector == nil {
		return o.run(ctx)
	}

	o.logger.Info("leader election enabled", "elector", o.leaderElector.Name())

	return o.leaderElector.Run(ctx, lease.LeaderCallbacks{
		OnStartedLeading: func(leaderCtx context.Context) {
			o.logger.Info("became leader, starting orchestrator")
			if err := o.run(leaderCtx); err != nil && leaderCtx.Err() == nil {
				o.logger.Error("orchestrator failed while leading", "error", err)
			}
		},
		OnStoppedLeading: func() {
			o.logger.Info("lost leadership, orchestrator paused")
		},
	})
}

// run contains the core orchestrator loop.
func (o *Orchestrator) run(ctx context.Context) error {
	// Run initial reconciliation if configured (no lock needed — nothing else running yet).
	if o.reconciler != nil {
		o.logger.Info("running initial reconciliation")
		if err := o.reconciler.Reconcile(ctx); err != nil {
			o.logger.Error("initial reconciliation failed", "error", err)
		}
	}

	// Start the trigger.
	events := make(chan core.RouteEvent)
	triggerErrCh := make(chan error, 1)
	go func() {
		triggerErrCh <- o.trigger.Start(ctx, events)
	}()

	// Start periodic reconciliation if interval > 0.
	if o.reconciler != nil && o.reconcileInterval > 0 {
		go o.reconcileLoop(ctx)
	}

	o.logger.Info("orchestrator started",
		"trigger", o.trigger.Name(),
		"issuer", o.issuer.Name(),
		"provisioner", o.provisioner.Name(),
	)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-triggerErrCh:
			return fmt.Errorf("trigger %s failed: %w", o.trigger.Name(), err)

		case event := <-events:
			o.handleEvent(ctx, event)
		}
	}
}

func (o *Orchestrator) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(o.reconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.mu.Lock()
			o.logger.Info("periodic reconciliation triggered")
			if err := o.reconciler.Reconcile(ctx); err != nil {
				o.logger.Error("periodic reconciliation failed", "error", err)
			}
			o.mu.Unlock()
		}
	}
}

func (o *Orchestrator) handleEvent(ctx context.Context, event core.RouteEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()

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
