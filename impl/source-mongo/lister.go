package sourcemongo

import (
	"context"
	"fmt"

	core "github.com/nicol/dynamic-route-provisioner/core"
	"github.com/nicol/dynamic-route-provisioner/core/desired"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Compile-time check.
var _ desired.DesiredStateProvider = (*MongoDesiredState)(nil)

// MongoDesiredState reads all documents from a MongoDB collection and converts
// them to RouteRequests using the developer-provided DocumentMapper.
type MongoDesiredState struct {
	collection *mongo.Collection
	mapper     DocumentMapper
}

// NewDesiredState creates a MongoDesiredState provider.
func NewDesiredState(collection *mongo.Collection, mapper DocumentMapper) *MongoDesiredState {
	return &MongoDesiredState{
		collection: collection,
		mapper:     mapper,
	}
}

// Name returns "mongo-desired-state".
func (s *MongoDesiredState) Name() string { return "mongo-desired-state" }

// List reads all documents from the collection and maps each to a RouteRequest.
func (s *MongoDesiredState) List(ctx context.Context) ([]core.RouteRequest, error) {
	cursor, err := s.collection.Find(ctx, bson.D{})
	if err != nil {
		return nil, fmt.Errorf("find documents: %w", err)
	}
	defer cursor.Close(ctx)

	var routes []core.RouteRequest
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("decode document: %w", err)
		}

		route, err := s.mapper.MapDocument(ctx, doc)
		if err != nil {
			return nil, fmt.Errorf("map document: %w", err)
		}

		if route != nil {
			routes = append(routes, *route)
		}
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	return routes, nil
}
