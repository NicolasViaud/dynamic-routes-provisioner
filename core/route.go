package core

import "time"

// EventType represents the kind of route lifecycle event.
type EventType string

const (
	EventRouteCreated EventType = "route.created"
	EventRouteUpdated EventType = "route.updated"
	EventRouteDeleted EventType = "route.deleted"
)

// RouteRequest describes a route that needs to be provisioned.
type RouteRequest struct {
	ID        string
	Host      string
	Path      string
	Backends  []Backend
	TLS       bool
	Metadata  map[string]string
	CreatedAt time.Time
}

// Backend is a target service for a route.
type Backend struct {
	ServiceName string
	Port        int
	Weight      int
}

// RouteEvent is emitted by a Trigger when a route change is detected.
type RouteEvent struct {
	Type    EventType
	Route   RouteRequest
	RawData []byte // original payload from the source
}

// Certificate holds the result of a certificate issuance.
type Certificate struct {
	Host        string
	CertPEM     []byte
	KeyPEM      []byte
	CACertPEM   []byte
	NotBefore   time.Time
	NotAfter    time.Time
	IssuerName  string
}

// ProvisionedRoute is the final output after a route has been created
// on the target gateway/proxy.
type ProvisionedRoute struct {
	RouteID       string
	Host          string
	GatewayID     string
	CertificateID string
	Status        string
	ProviderName  string
}
