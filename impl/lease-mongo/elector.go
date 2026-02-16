package leasemongo

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/nicol/dynamic-route-provisioner/core/lease"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Compile-time check.
var _ lease.LeaderElector = (*MongoLeaderElector)(nil)

// MongoLeaderElector implements leader election using a MongoDB document.
// A single document with _id equal to the lease name is used. The holder
// field identifies the current leader, and expiresAt determines when the
// lease can be taken over.
type MongoLeaderElector struct {
	collection *mongo.Collection
	cfg        config
	leading    atomic.Bool
}

// New creates a MongoLeaderElector. The collection is used to store the
// lease document.
func New(collection *mongo.Collection, opts ...Option) *MongoLeaderElector {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}

	return &MongoLeaderElector{
		collection: collection,
		cfg:        cfg,
	}
}

// Name returns "mongo-lease".
func (e *MongoLeaderElector) Name() string { return "mongo-lease" }

// IsLeader reports whether this instance currently holds the lease.
func (e *MongoLeaderElector) IsLeader() bool { return e.leading.Load() }

// Run starts the leader election loop. It blocks until ctx is cancelled.
func (e *MongoLeaderElector) Run(ctx context.Context, cb lease.LeaderCallbacks) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		acquired, err := e.tryAcquire(ctx)
		if err != nil {
			return fmt.Errorf("acquire lease: %w", err)
		}

		if acquired {
			e.leading.Store(true)

			leaderCtx, cancelLeader := context.WithCancel(ctx)

			// Run the leader callback in a goroutine.
			done := make(chan struct{})
			go func() {
				cb.OnStartedLeading(leaderCtx)
				close(done)
			}()

			// Renew loop — runs until leadership is lost or ctx cancelled.
			e.renewLoop(ctx, cancelLeader)

			// Leadership lost.
			e.leading.Store(false)
			cancelLeader()
			<-done

			if cb.OnStoppedLeading != nil {
				cb.OnStoppedLeading()
			}

			// Best-effort release so another instance can take over immediately.
			e.release(context.Background())

			if ctx.Err() != nil {
				return ctx.Err()
			}
		}

		// Wait before retrying.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.cfg.retryInterval):
		}
	}
}

// tryAcquire attempts to acquire or reclaim the lease atomically.
func (e *MongoLeaderElector) tryAcquire(ctx context.Context) (bool, error) {
	now := time.Now()
	filter := bson.D{
		{Key: "_id", Value: e.cfg.leaseName},
		{Key: "$or", Value: bson.A{
			bson.D{{Key: "expiresAt", Value: bson.D{{Key: "$lt", Value: now}}}},
			bson.D{{Key: "holder", Value: e.cfg.identity}},
		}},
	}

	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "holder", Value: e.cfg.identity},
		{Key: "acquiredAt", Value: now},
		{Key: "renewedAt", Value: now},
		{Key: "expiresAt", Value: now.Add(e.cfg.leaseDuration)},
	}}}

	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	result := e.collection.FindOneAndUpdate(ctx, filter, update, opts)
	if result.Err() != nil {
		// If another instance already holds the lease, the filter won't match
		// and with upsert a duplicate key error occurs — that means we lost.
		if mongo.IsDuplicateKeyError(result.Err()) {
			return false, nil
		}
		return false, result.Err()
	}

	return true, nil
}

// renewLoop renews the lease periodically. If a renewal fails (e.g. another
// instance took over), it calls cancelLeader and returns.
func (e *MongoLeaderElector) renewLoop(ctx context.Context, cancelLeader context.CancelFunc) {
	ticker := time.NewTicker(e.cfg.renewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewed, err := e.tryRenew(ctx)
			if err != nil || !renewed {
				cancelLeader()
				return
			}
		}
	}
}

// tryRenew extends the lease expiry. Returns false if this instance is no
// longer the holder.
func (e *MongoLeaderElector) tryRenew(ctx context.Context) (bool, error) {
	now := time.Now()
	filter := bson.D{
		{Key: "_id", Value: e.cfg.leaseName},
		{Key: "holder", Value: e.cfg.identity},
	}

	update := bson.D{{Key: "$set", Value: bson.D{
		{Key: "renewedAt", Value: now},
		{Key: "expiresAt", Value: now.Add(e.cfg.leaseDuration)},
	}}}

	result, err := e.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, err
	}

	return result.MatchedCount > 0, nil
}

// release removes the lease document so another instance can acquire immediately.
func (e *MongoLeaderElector) release(ctx context.Context) {
	filter := bson.D{
		{Key: "_id", Value: e.cfg.leaseName},
		{Key: "holder", Value: e.cfg.identity},
	}
	e.collection.DeleteOne(ctx, filter)
}
