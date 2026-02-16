package sourcemongo

import (
	"context"
	"fmt"

	core "github.com/nicol/dynamic-route-provisioner/core"
	"github.com/nicol/dynamic-route-provisioner/core/trigger"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Compile-time check that MongoTrigger satisfies trigger.Trigger.
var _ trigger.Trigger = (*MongoTrigger)(nil)

// MongoTrigger watches a MongoDB collection change stream and converts
// change events into core.RouteEvents using a developer-provided DocumentMapper.
type MongoTrigger struct {
	collection *mongo.Collection
	mapper     DocumentMapper
	cfg        config
}

// New creates a MongoTrigger. The caller provides the collection to watch and
// a DocumentMapper that defines the filtering pipeline and event mapping.
func New(collection *mongo.Collection, mapper DocumentMapper, opts ...Option) *MongoTrigger {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	return &MongoTrigger{
		collection: collection,
		mapper:     mapper,
		cfg:        cfg,
	}
}

// Name returns "mongo-changestream".
func (t *MongoTrigger) Name() string { return "mongo-changestream" }

// Start opens the change stream and sends mapped events to the channel.
// It blocks until the context is cancelled or a fatal error occurs.
func (t *MongoTrigger) Start(ctx context.Context, events chan<- core.RouteEvent) error {
	csOpts := options.ChangeStream().
		SetFullDocument(t.cfg.fullDocument)

	if t.cfg.resumeAfter != nil {
		csOpts.SetResumeAfter(t.cfg.resumeAfter)
	}
	if t.cfg.startAfter != nil {
		csOpts.SetStartAfter(t.cfg.startAfter)
	}

	pipeline := t.mapper.Pipeline()
	cs, err := t.collection.Watch(ctx, pipeline, csOpts)
	if err != nil {
		return fmt.Errorf("open change stream: %w", err)
	}
	defer cs.Close(ctx)

	for cs.Next(ctx) {
		var raw bson.M
		if err := cs.Decode(&raw); err != nil {
			return fmt.Errorf("decode change event: %w", err)
		}

		routeEvent, err := t.mapper.MapEvent(ctx, raw)
		if err != nil {
			return fmt.Errorf("map change event: %w", err)
		}

		// nil means "skip this event"
		if routeEvent == nil {
			continue
		}

		select {
		case events <- *routeEvent:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// cs.Next returned false — check for errors
	if err := cs.Err(); err != nil {
		return fmt.Errorf("change stream error: %w", err)
	}
	return nil
}
