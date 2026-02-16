package triggermongo

import (
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Option configures a MongoTrigger.
type Option func(*config)

type config struct {
	fullDocument options.FullDocument
	resumeAfter  bson.Raw
	startAfter   bson.Raw
}

func defaultConfig() config {
	return config{
		fullDocument: options.UpdateLookup,
	}
}

// WithFullDocument sets the fullDocument option on the change stream.
// See options.FullDocument constants: Default, UpdateLookup, WhenAvailable, Required.
func WithFullDocument(mode options.FullDocument) Option {
	return func(c *config) {
		c.fullDocument = mode
	}
}

// WithResumeAfter resumes the change stream after the given resume token,
// allowing the trigger to pick up where it left off.
func WithResumeAfter(token bson.Raw) Option {
	return func(c *config) {
		c.resumeAfter = token
	}
}

// WithStartAfter starts the change stream after the given resume token.
// Unlike ResumeAfter, StartAfter can resume from an invalidate event.
func WithStartAfter(token bson.Raw) Option {
	return func(c *config) {
		c.startAfter = token
	}
}
