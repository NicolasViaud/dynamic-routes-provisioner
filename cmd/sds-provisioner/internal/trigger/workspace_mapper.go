package trigger

import (
	"context"
	"fmt"
	"net/url"
	"time"

	core "github.com/nicol/dynamic-route-provisioner/core"
	triggermongo "github.com/nicol/dynamic-route-provisioner/trigger-mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Compile-time check.
var _ triggermongo.DocumentMapper = (*WorkspaceMapper)(nil)

// WorkspaceMapper watches the workspace collection for documents containing
// a "url" field and converts them into RouteEvents.
type WorkspaceMapper struct{}

// Pipeline filters change events to only receive inserts, updates, and deletes.
func (m *WorkspaceMapper) Pipeline() mongo.Pipeline {
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
func (m *WorkspaceMapper) MapEvent(_ context.Context, changeEvent bson.M) (*core.RouteEvent, error) {
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

	rawURL, ok := fullDoc["url"].(string)
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
			Path:      "/",
			TLS:       true,
			Metadata:  map[string]string{"source_url": rawURL},
			CreatedAt: time.Now(),
		},
	}, nil
}

// MapDocument converts a full workspace document into a RouteRequest.
// Used by the desired state provider during reconciliation.
func (m *WorkspaceMapper) MapDocument(_ context.Context, doc bson.M) (*core.RouteRequest, error) {
	rawURL, ok := doc["url"].(string)
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
		Path:     "/",
		TLS:      true,
		Metadata: map[string]string{"source_url": rawURL},
	}, nil
}
