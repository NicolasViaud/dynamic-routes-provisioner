package trigger

import (
	"context"
	"strings"
	"sync"

	core "github.com/NicolasViaud/dynamic-route-provisioner/core"
)

// Compile-time check.
var _ Trigger = (*CompositeTrigger)(nil)

// CompositeTrigger fans-in multiple triggers into a single event channel.
// It implements the Trigger interface so the orchestrator can treat N
// underlying triggers as one.
type CompositeTrigger struct {
	triggers []Trigger
}

// NewComposite creates a CompositeTrigger from the given triggers.
func NewComposite(triggers ...Trigger) *CompositeTrigger {
	return &CompositeTrigger{triggers: triggers}
}

// Name returns a composite name listing all child triggers.
func (c *CompositeTrigger) Name() string {
	names := make([]string, len(c.triggers))
	for i, t := range c.triggers {
		names[i] = t.Name()
	}
	return "composite(" + strings.Join(names, ", ") + ")"
}

// Start launches all child triggers concurrently. Each child writes to the
// shared events channel. Start blocks until all children return or the context
// is cancelled. It returns the first non-nil, non-context error encountered.
func (c *CompositeTrigger) Start(ctx context.Context, events chan<- core.RouteEvent) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg      sync.WaitGroup
		once    sync.Once
		firstErr error
	)

	for _, t := range c.triggers {
		wg.Add(1)
		go func(t Trigger) {
			defer wg.Done()
			if err := t.Start(ctx, events); err != nil && ctx.Err() == nil {
				once.Do(func() {
					firstErr = err
					cancel()
				})
			}
		}(t)
	}

	wg.Wait()
	return firstErr
}
