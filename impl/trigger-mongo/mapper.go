package triggermongo

import (
	"context"

	core "github.com/nicol/dynamic-route-provisioner/core"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// DocumentMapper is the abstraction that developers implement to adapt the
// MongoDB change stream to their own document schema.
type DocumentMapper interface {
	// Pipeline returns an aggregation pipeline that filters which change
	// events are delivered by the change stream. Return nil to receive all
	// changes on the watched collection.
	Pipeline() mongo.Pipeline

	// MapEvent converts a raw change-stream event document into a
	// core.RouteEvent. Return (nil, nil) to silently skip an event that is
	// not relevant.
	MapEvent(ctx context.Context, changeEvent bson.M) (*core.RouteEvent, error)
}
