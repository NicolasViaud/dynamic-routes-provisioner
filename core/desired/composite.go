package desired

import (
	"context"
	"strings"

	core "github.com/NicolasViaud/dynamic-route-provisioner/core"
)

// Compile-time check.
var _ DesiredStateProvider = (*CompositeDesiredState)(nil)

// CompositeDesiredState merges the results of multiple DesiredStateProviders
// into a single list. It implements the DesiredStateProvider interface so the
// reconciler can treat N underlying providers as one.
type CompositeDesiredState struct {
	providers []DesiredStateProvider
}

// NewComposite creates a CompositeDesiredState from the given providers.
func NewComposite(providers ...DesiredStateProvider) *CompositeDesiredState {
	return &CompositeDesiredState{providers: providers}
}

// Name returns a composite name listing all child providers.
func (c *CompositeDesiredState) Name() string {
	names := make([]string, len(c.providers))
	for i, p := range c.providers {
		names[i] = p.Name()
	}
	return "composite(" + strings.Join(names, ", ") + ")"
}

// List calls each child provider's List and concatenates the results.
func (c *CompositeDesiredState) List(ctx context.Context) ([]core.RouteRequest, error) {
	var all []core.RouteRequest
	for _, p := range c.providers {
		routes, err := p.List(ctx)
		if err != nil {
			return nil, err
		}
		all = append(all, routes...)
	}
	return all, nil
}
