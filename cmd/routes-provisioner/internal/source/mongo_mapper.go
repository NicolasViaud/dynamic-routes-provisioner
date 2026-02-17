package source

import (
	"context"
	"fmt"
	"net/url"
	"time"

	core "github.com/nicol/dynamic-route-provisioner/core"
	sourcemongo "github.com/nicol/dynamic-route-provisioner/source-mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Compile-time check.
var _ sourcemongo.DocumentMapper = (*MongoMapper)(nil)

// MongoMapper watches the workspace collection for documents containing
// a URL field and converts them into RouteEvents. Field name, path, and TLS
// behaviour are driven by configuration.
type MongoMapper struct {
	URLField string         // document field containing the URL
	Path     string         // default route path
	TLS      bool           // whether to enable TLS
	Backends []core.Backend // fixed backends applied to every route
}

// Pipeline filters change events to only receive inserts, updates, and deletes.
func (m *MongoMapper) Pipeline() mongo.Pipeline {
	return mongo.Pipeline{
		{{Key: "$match", Value: bson.D{
			{Key: "operationType", Value: bson.D{
				{Key: "$in", Value: bson.A{"insert", "update", "replace", "delete"}},
			}},
		}}},
	}
}

// MapEvent converts a MongoDB change stream event into a RouteEvent by
// reading the "url" field from the workspace document.
func (m *MongoMapper) MapEvent(_ context.Context, changeEvent bson.M) (*core.RouteEvent, error) {
	opType, _ := changeEvent["operationType"].(string)

	// Handle deletes — no fullDocument available.
	if opType == "delete" {
		docKey, ok := changeEvent["documentKey"].(bson.M)
		if !ok {
			return nil, nil
		}
		id := fmt.Sprintf("%v", docKey["_id"])
		return &core.RouteEvent{
			Type: core.EventRouteDeleted,
			Route: core.RouteRequest{
				ID: id,
			},
		}, nil
	}

	// For insert/update/replace, read the full document.
	fullDoc, ok := changeEvent["fullDocument"].(bson.M)
	if !ok {
		return nil, nil
	}

	rawURL, ok := fullDoc[m.URLField].(string)
	if !ok || rawURL == "" {
		return nil, nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url %q: %w", rawURL, err)
	}

	host := parsed.Hostname()
	if host == "" {
		return nil, nil
	}

	id := fmt.Sprintf("%v", fullDoc["_id"])

	eventType := core.EventRouteCreated
	if opType == "update" || opType == "replace" {
		eventType = core.EventRouteUpdated
	}

	return &core.RouteEvent{
		Type: eventType,
		Route: core.RouteRequest{
			ID:        id,
			Host:      host,
			Path:      m.Path,
			Backends:  m.Backends,
			TLS:       m.TLS,
			Metadata:  map[string]string{"source_url": rawURL},
			CreatedAt: time.Now(),
		},
	}, nil
}

// MapDocument converts a full workspace document into a RouteRequest.
// Used by the desired state provider during reconciliation.
func (m *MongoMapper) MapDocument(_ context.Context, doc bson.M) (*core.RouteRequest, error) {
	rawURL, ok := doc[m.URLField].(string)
	if !ok || rawURL == "" {
		return nil, nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url %q: %w", rawURL, err)
	}

	host := parsed.Hostname()
	if host == "" {
		return nil, nil
	}

	id := fmt.Sprintf("%v", doc["_id"])

	return &core.RouteRequest{
		ID:       id,
		Host:     host,
		Path:     m.Path,
		Backends: m.Backends,
		TLS:      m.TLS,
		Metadata: map[string]string{"source_url": rawURL},
	}, nil
}
