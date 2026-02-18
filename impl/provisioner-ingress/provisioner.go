package provingress

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	core "github.com/NicolasViaud/dynamic-route-provisioner/core"
	"github.com/NicolasViaud/dynamic-route-provisioner/core/provisioner"
	networkingv1 "k8s.io/api/networking/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// Compile-time check that IngressProvisioner satisfies provisioner.RouteProvisioner.
var _ provisioner.RouteProvisioner = (*IngressProvisioner)(nil)

// IngressProvisioner manages Kubernetes Ingress resources by packing multiple
// routes as IngressRules into a configurable number of Ingress "buckets".
// JSON Patch is used for efficient add/remove operations.
type IngressProvisioner struct {
	clientset kubernetes.Interface
	mapper    IngressMapper
	cfg       config
	logger    *slog.Logger
}

// New creates an IngressProvisioner. The mapper defines how RouteRequests
// translate to IngressRules; options configure the Kubernetes client and
// packing strategy.
func New(mapper IngressMapper, logger *slog.Logger, opts ...Option) (*IngressProvisioner, error) {
	cfg := config{
		namespace:           "default",
		maxRoutesPerIngress: 50,
		managementLabel:     "app.kubernetes.io/managed-by",
		managementValue:     "dynamic-route-provisioner",
	}
	for _, o := range opts {
		if err := o(&cfg); err != nil {
			return nil, fmt.Errorf("apply option: %w", err)
		}
	}

	if cfg.clientset == nil {
		return nil, fmt.Errorf("kubernetes client not configured: use WithKubeClient, WithInClusterConfig, or WithKubeConfig")
	}

	return &IngressProvisioner{
		clientset: cfg.clientset,
		mapper:    mapper,
		cfg:       cfg,
		logger:    logger,
	}, nil
}

// Name returns "ingress-k8s".
func (p *IngressProvisioner) Name() string { return "ingress-k8s" }

// Provision creates or updates a route by adding an IngressRule to an
// available Ingress resource. If all existing Ingresses are at capacity a new
// one is created.
func (p *IngressProvisioner) Provision(ctx context.Context, req core.RouteRequest, cert *core.Certificate) (*core.ProvisionedRoute, error) {
	routeID := p.mapper.RouteID(req)

	rule, err := p.mapper.MapRule(req)
	if err != nil {
		return nil, fmt.Errorf("map rule: %w", err)
	}

	var tlsEntry *networkingv1.IngressTLS
	if req.TLS {
		tlsEntry, err = p.mapper.MapTLS(req)
		if err != nil {
			return nil, fmt.Errorf("map tls: %w", err)
		}
	}

	// Check if the route already exists in a managed Ingress.
	ingresses, err := p.listManagedIngresses(ctx)
	if err != nil {
		return nil, err
	}

	for i := range ingresses {
		for j, existing := range ingresses[i].Spec.Rules {
			if p.mapper.ExtractRouteID(existing) == routeID {
				return p.updateRule(ctx, &ingresses[i], j, rule, tlsEntry, req)
			}
		}
	}

	// Find an Ingress with available capacity, or create a new one with the
	// rule embedded (Kubernetes rejects Ingresses with neither rules nor defaultBackend).
	var ingressName string
	if existing := p.findWithCapacity(ingresses); existing != nil {
		if err := p.patchAddRule(ctx, existing.Name, rule, tlsEntry); err != nil {
			return nil, fmt.Errorf("patch add rule: %w", err)
		}
		ingressName = existing.Name
	} else {
		var tls []networkingv1.IngressTLS
		if tlsEntry != nil {
			tls = []networkingv1.IngressTLS{*tlsEntry}
		}
		created, err := p.createIngressWithRules(ctx, len(ingresses), []networkingv1.IngressRule{rule}, tls)
		if err != nil {
			return nil, err
		}
		ingressName = created.Name
	}

	p.logger.Info("provisioned route", "routeID", routeID, "ingress", ingressName)

	return p.buildProvisionedRoute(routeID, req.Host, ingressName, cert), nil
}

// Deprovision removes a route by deleting its IngressRule from the containing
// Ingress. If the Ingress becomes empty it is deleted entirely.
func (p *IngressProvisioner) Deprovision(ctx context.Context, routeID string) error {
	ingresses, err := p.listManagedIngresses(ctx)
	if err != nil {
		return err
	}

	for i := range ingresses {
		for j, rule := range ingresses[i].Spec.Rules {
			if p.mapper.ExtractRouteID(rule) != routeID {
				continue
			}

			// Last rule — delete the whole Ingress.
			if len(ingresses[i].Spec.Rules) == 1 {
				if err := p.deleteIngress(ctx, ingresses[i].Name); err != nil {
					return err
				}
				p.logger.Info("deleted empty ingress", "ingress", ingresses[i].Name)
				return nil
			}

			if err := p.patchRemoveRule(ctx, &ingresses[i], j); err != nil {
				return fmt.Errorf("patch remove rule: %w", err)
			}
			p.logger.Info("deprovisioned route", "routeID", routeID, "ingress", ingresses[i].Name)
			return nil
		}
	}

	// Route not found — idempotent success.
	return nil
}

// List returns all provisioned routes by querying managed Ingress resources
// via label selector and extracting ProvisionedRoute from their rules.
func (p *IngressProvisioner) List(ctx context.Context) ([]core.ProvisionedRoute, error) {
	ingresses, err := p.listManagedIngresses(ctx)
	if err != nil {
		return nil, err
	}

	var routes []core.ProvisionedRoute
	for _, ing := range ingresses {
		tlsHosts := p.collectTLSHosts(ing)
		for _, rule := range ing.Spec.Rules {
			routeID := p.mapper.ExtractRouteID(rule)
			host := rule.Host

			certID := ""
			if _, ok := tlsHosts[host]; ok {
				certID = host
			}

			routes = append(routes, core.ProvisionedRoute{
				RouteID:       routeID,
				Host:          host,
				GatewayID:     fmt.Sprintf("%s/%s", p.cfg.namespace, ing.Name),
				CertificateID: certID,
				Status:        "active",
				ProviderName:  p.Name(),
			})
		}
	}

	return routes, nil
}

// BatchProvision packs multiple routes into Ingresses efficiently:
// 1. Maps all routes to IngressRules upfront.
// 2. Fills existing Ingresses that have capacity.
// 3. Creates new Ingresses for overflow.
// 4. Issues one patch per Ingress.
func (p *IngressProvisioner) BatchProvision(ctx context.Context, routes []core.RouteRequest, certs map[string]*core.Certificate) ([]core.ProvisionedRoute, error) {
	type mappedRoute struct {
		req core.RouteRequest
		rule networkingv1.IngressRule
		tls  *networkingv1.IngressTLS
	}

	mapped := make([]mappedRoute, 0, len(routes))
	for _, req := range routes {
		rule, err := p.mapper.MapRule(req)
		if err != nil {
			return nil, fmt.Errorf("map route %s: %w", req.ID, err)
		}
		var tlsEntry *networkingv1.IngressTLS
		if req.TLS {
			tlsEntry, err = p.mapper.MapTLS(req)
			if err != nil {
				return nil, fmt.Errorf("map tls %s: %w", req.ID, err)
			}
		}
		mapped = append(mapped, mappedRoute{req: req, rule: rule, tls: tlsEntry})
	}

	ingresses, err := p.listManagedIngresses(ctx)
	if err != nil {
		return nil, err
	}

	// Sort Ingresses by available capacity (most room first).
	sort.Slice(ingresses, func(i, j int) bool {
		return len(ingresses[i].Spec.Rules) < len(ingresses[j].Spec.Rules)
	})

	// Pack routes into Ingresses.
	// needsPatch is false for newly-created Ingresses whose rules are already embedded.
	type batch struct {
		ingressName string
		routes      []mappedRoute
		needsPatch  bool
	}
	var batches []batch
	cursor := 0

	for _, ing := range ingresses {
		if cursor >= len(mapped) {
			break
		}
		capacity := p.cfg.maxRoutesPerIngress - len(ing.Spec.Rules)
		if capacity <= 0 {
			continue
		}
		end := cursor + capacity
		if end > len(mapped) {
			end = len(mapped)
		}
		batches = append(batches, batch{ingressName: ing.Name, routes: mapped[cursor:end], needsPatch: true})
		cursor = end
	}

	// Create new Ingresses for remaining routes with rules embedded at creation
	// time — Kubernetes rejects Ingresses with neither rules nor defaultBackend.
	newIndex := len(ingresses)
	for cursor < len(mapped) {
		end := cursor + p.cfg.maxRoutesPerIngress
		if end > len(mapped) {
			end = len(mapped)
		}

		batchRoutes := mapped[cursor:end]
		rules := make([]networkingv1.IngressRule, len(batchRoutes))
		var tls []networkingv1.IngressTLS
		for i, mr := range batchRoutes {
			rules[i] = mr.rule
			if mr.tls != nil {
				tls = append(tls, *mr.tls)
			}
		}

		ing, err := p.createIngressWithRules(ctx, newIndex, rules, tls)
		if err != nil {
			return nil, err
		}
		batches = append(batches, batch{ingressName: ing.Name, routes: batchRoutes, needsPatch: false})
		cursor = end
		newIndex++
	}

	// Apply patches for existing Ingresses; newly created ones already have their rules.
	var results []core.ProvisionedRoute
	for _, b := range batches {
		if b.needsPatch {
			rules := make([]networkingv1.IngressRule, len(b.routes))
			var tlsEntries []networkingv1.IngressTLS
			for i, mr := range b.routes {
				rules[i] = mr.rule
				if mr.tls != nil {
					tlsEntries = append(tlsEntries, *mr.tls)
				}
			}
			if err := p.patchAddRules(ctx, b.ingressName, rules, tlsEntries); err != nil {
				return nil, fmt.Errorf("patch ingress %s: %w", b.ingressName, err)
			}
		}

		for _, mr := range b.routes {
			cert := certs[mr.req.Host]
			results = append(results, *p.buildProvisionedRoute(
				p.mapper.RouteID(mr.req), mr.req.Host, b.ingressName, cert,
			))
		}
	}

	p.logger.Info("batch provisioned routes", "count", len(results))
	return results, nil
}

// BatchDeprovision removes multiple routes efficiently by grouping removals
// per Ingress, patching each once, and deleting Ingresses that become empty.
func (p *IngressProvisioner) BatchDeprovision(ctx context.Context, routeIDs []string) error {
	ingresses, err := p.listManagedIngresses(ctx)
	if err != nil {
		return err
	}

	// Build a lookup: routeID → true.
	toRemove := make(map[string]bool, len(routeIDs))
	for _, id := range routeIDs {
		toRemove[id] = true
	}

	for i := range ingresses {
		var keepRules []networkingv1.IngressRule
		var keepTLS []networkingv1.IngressTLS
		removedHosts := make(map[string]bool)
		changed := false

		for _, rule := range ingresses[i].Spec.Rules {
			if toRemove[p.mapper.ExtractRouteID(rule)] {
				changed = true
				removedHosts[rule.Host] = true
			} else {
				keepRules = append(keepRules, rule)
			}
		}

		if !changed {
			continue
		}

		// Rebuild TLS entries, excluding removed hosts.
		for _, tls := range ingresses[i].Spec.TLS {
			var hosts []string
			for _, h := range tls.Hosts {
				if !removedHosts[h] {
					hosts = append(hosts, h)
				}
			}
			if len(hosts) > 0 {
				tls.Hosts = hosts
				keepTLS = append(keepTLS, tls)
			}
		}

		if len(keepRules) == 0 {
			if err := p.deleteIngress(ctx, ingresses[i].Name); err != nil {
				return err
			}
			p.logger.Info("deleted empty ingress", "ingress", ingresses[i].Name)
			continue
		}

		if err := p.patchReplaceSpec(ctx, ingresses[i].Name, keepRules, keepTLS); err != nil {
			return fmt.Errorf("patch ingress %s: %w", ingresses[i].Name, err)
		}
	}

	p.logger.Info("batch deprovisioned routes", "count", len(routeIDs))
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (p *IngressProvisioner) labelSelector() string {
	return fmt.Sprintf("%s=%s", p.cfg.managementLabel, p.cfg.managementValue)
}

func (p *IngressProvisioner) listManagedIngresses(ctx context.Context) ([]networkingv1.Ingress, error) {
	list, err := p.clientset.NetworkingV1().Ingresses(p.cfg.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: p.labelSelector(),
	})
	if err != nil {
		return nil, fmt.Errorf("list managed ingresses: %w", err)
	}
	return list.Items, nil
}

func (p *IngressProvisioner) findWithCapacity(ingresses []networkingv1.Ingress) *networkingv1.Ingress {
	for i := range ingresses {
		if len(ingresses[i].Spec.Rules) < p.cfg.maxRoutesPerIngress {
			return &ingresses[i]
		}
	}
	return nil
}

// createIngressWithRules creates a new Ingress pre-populated with the given rules
// and TLS entries. Kubernetes requires at least one rule or a defaultBackend, so
// callers must always supply at least one rule.
func (p *IngressProvisioner) createIngressWithRules(ctx context.Context, index int, rules []networkingv1.IngressRule, tls []networkingv1.IngressTLS) (*networkingv1.Ingress, error) {
	ingressClass := p.mapper.IngressClass()
	var ingressClassName *string
	if ingressClass != "" {
		ingressClassName = &ingressClass
	}

	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        p.mapper.IngressName(index),
			Namespace:   p.cfg.namespace,
			Labels:      p.mapper.Labels(),
			Annotations: p.mapper.Annotations(),
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ingressClassName,
			Rules:            rules,
			TLS:              tls,
		},
	}

	created, err := p.clientset.NetworkingV1().Ingresses(p.cfg.namespace).Create(ctx, ing, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create ingress: %w", err)
	}
	p.logger.Info("created ingress", "name", created.Name)
	return created, nil
}

func (p *IngressProvisioner) deleteIngress(ctx context.Context, name string) error {
	err := p.clientset.NetworkingV1().Ingresses(p.cfg.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !k8serr.IsNotFound(err) {
		return fmt.Errorf("delete ingress %s: %w", name, err)
	}
	return nil
}

// patchAddRule appends a single rule (and optional TLS entry) via JSON Patch.
func (p *IngressProvisioner) patchAddRule(ctx context.Context, ingressName string, rule networkingv1.IngressRule, tls *networkingv1.IngressTLS) error {
	patches := []jsonPatch{
		{Op: "add", Path: "/spec/rules/-", Value: rule},
	}
	if tls != nil {
		patches = append(patches, jsonPatch{Op: "add", Path: "/spec/tls/-", Value: *tls})
	}
	return p.applyJSONPatch(ctx, ingressName, patches)
}

// patchAddRules appends multiple rules and TLS entries via JSON Patch.
func (p *IngressProvisioner) patchAddRules(ctx context.Context, ingressName string, rules []networkingv1.IngressRule, tlsEntries []networkingv1.IngressTLS) error {
	patches := make([]jsonPatch, 0, len(rules)+len(tlsEntries))
	for _, r := range rules {
		patches = append(patches, jsonPatch{Op: "add", Path: "/spec/rules/-", Value: r})
	}
	for _, t := range tlsEntries {
		patches = append(patches, jsonPatch{Op: "add", Path: "/spec/tls/-", Value: t})
	}
	return p.applyJSONPatch(ctx, ingressName, patches)
}

// patchRemoveRule removes a rule at the given index and its matching TLS entry.
func (p *IngressProvisioner) patchRemoveRule(ctx context.Context, ing *networkingv1.Ingress, ruleIndex int) error {
	host := ing.Spec.Rules[ruleIndex].Host

	patches := []jsonPatch{
		{Op: "remove", Path: fmt.Sprintf("/spec/rules/%d", ruleIndex)},
	}

	// Find and remove the matching TLS entry.
	for i, tls := range ing.Spec.TLS {
		for _, h := range tls.Hosts {
			if h == host {
				patches = append(patches, jsonPatch{Op: "remove", Path: fmt.Sprintf("/spec/tls/%d", i)})
				goto done
			}
		}
	}
done:
	return p.applyJSONPatch(ctx, ing.Name, patches)
}

// patchReplaceSpec replaces the entire rules and tls arrays (used in batch deprovision).
func (p *IngressProvisioner) patchReplaceSpec(ctx context.Context, ingressName string, rules []networkingv1.IngressRule, tls []networkingv1.IngressTLS) error {
	patches := []jsonPatch{
		{Op: "replace", Path: "/spec/rules", Value: rules},
		{Op: "replace", Path: "/spec/tls", Value: tls},
	}
	return p.applyJSONPatch(ctx, ingressName, patches)
}

// updateRule replaces an existing rule in-place via JSON Patch.
func (p *IngressProvisioner) updateRule(ctx context.Context, ing *networkingv1.Ingress, ruleIndex int, rule networkingv1.IngressRule, tls *networkingv1.IngressTLS, req core.RouteRequest) (*core.ProvisionedRoute, error) {
	patches := []jsonPatch{
		{Op: "replace", Path: fmt.Sprintf("/spec/rules/%d", ruleIndex), Value: rule},
	}

	if tls != nil {
		host := ing.Spec.Rules[ruleIndex].Host
		found := false
		for i, t := range ing.Spec.TLS {
			for _, h := range t.Hosts {
				if h == host {
					patches = append(patches, jsonPatch{Op: "replace", Path: fmt.Sprintf("/spec/tls/%d", i), Value: *tls})
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			patches = append(patches, jsonPatch{Op: "add", Path: "/spec/tls/-", Value: *tls})
		}
	}

	if err := p.applyJSONPatch(ctx, ing.Name, patches); err != nil {
		return nil, fmt.Errorf("patch update rule: %w", err)
	}

	return p.buildProvisionedRoute(p.mapper.RouteID(req), req.Host, ing.Name, nil), nil
}

func (p *IngressProvisioner) collectTLSHosts(ing networkingv1.Ingress) map[string]bool {
	hosts := make(map[string]bool)
	for _, tls := range ing.Spec.TLS {
		for _, h := range tls.Hosts {
			hosts[h] = true
		}
	}
	return hosts
}

func (p *IngressProvisioner) buildProvisionedRoute(routeID, host, ingressName string, cert *core.Certificate) *core.ProvisionedRoute {
	certID := ""
	if cert != nil {
		certID = cert.Host
	}
	return &core.ProvisionedRoute{
		RouteID:       routeID,
		Host:          host,
		GatewayID:     fmt.Sprintf("%s/%s", p.cfg.namespace, ingressName),
		CertificateID: certID,
		Status:        "active",
		ProviderName:  p.Name(),
	}
}

// ---------------------------------------------------------------------------
// JSON Patch helpers
// ---------------------------------------------------------------------------

type jsonPatch struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func (p *IngressProvisioner) applyJSONPatch(ctx context.Context, ingressName string, patches []jsonPatch) error {
	data, err := json.Marshal(patches)
	if err != nil {
		return fmt.Errorf("marshal json patch: %w", err)
	}

	_, err = p.clientset.NetworkingV1().Ingresses(p.cfg.namespace).Patch(
		ctx,
		ingressName,
		types.JSONPatchType,
		data,
		metav1.PatchOptions{},
	)
	return err
}
