package sourcehttp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	core "github.com/nicol/dynamic-route-provisioner/core"
	"github.com/nicol/dynamic-route-provisioner/core/desired"
	"github.com/nicol/dynamic-route-provisioner/core/trigger"
)

// Compile-time checks.
var (
	_ trigger.Trigger              = (*HTTPSource)(nil)
	_ desired.DesiredStateProvider = (*HTTPSource)(nil)
)

// HTTPSource implements both trigger.Trigger and desired.DesiredStateProvider
// via an HTTP API. Useful for manual testing and development without a real
// data source like MongoDB.
//
// Endpoints:
//
//	POST   /events          — push a RouteEvent into the trigger channel
//	GET    /events          — list events sent so far
//	POST   /routes          — add a RouteRequest to desired state
//	DELETE /routes/{host}   — remove a route from desired state
//	GET    /routes          — list current desired state
type HTTPSource struct {
	mu     sync.RWMutex
	routes map[string]core.RouteRequest // keyed by host
	events []core.RouteEvent
	eventC chan core.RouteEvent // set when Start is called
	logger *slog.Logger
}

// New creates an HTTPSource.
func New(logger *slog.Logger) *HTTPSource {
	if logger == nil {
		logger = slog.Default()
	}
	return &HTTPSource{
		routes: make(map[string]core.RouteRequest),
		logger: logger,
	}
}

// Name returns "http-source".
func (s *HTTPSource) Name() string { return "http-source" }

// Handler returns the http.Handler that serves the API endpoints.
// Mount this on your mux or run it with http.ListenAndServe.
func (s *HTTPSource) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", s.handlePostEvent)
	mux.HandleFunc("GET /events", s.handleGetEvents)
	mux.HandleFunc("POST /routes", s.handlePostRoute)
	mux.HandleFunc("DELETE /routes/{host}", s.handleDeleteRoute)
	mux.HandleFunc("GET /routes", s.handleGetRoutes)
	mux.HandleFunc("GET /openapi.json", handleOpenAPISpec)
	mux.HandleFunc("GET /swagger", handleSwaggerUI)
	return mux
}

// Start implements trigger.Trigger. It blocks until the context is cancelled,
// forwarding events pushed via POST /events to the channel.
func (s *HTTPSource) Start(ctx context.Context, events chan<- core.RouteEvent) error {
	s.mu.Lock()
	s.eventC = make(chan core.RouteEvent, 64)
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.eventC = nil
		s.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-s.eventC:
			select {
			case events <- ev:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// List implements desired.DesiredStateProvider.
func (s *HTTPSource) List(ctx context.Context) ([]core.RouteRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	routes := make([]core.RouteRequest, 0, len(s.routes))
	for _, r := range s.routes {
		routes = append(routes, r)
	}
	return routes, nil
}

// --- HTTP handlers ---

func (s *HTTPSource) handlePostEvent(w http.ResponseWriter, r *http.Request) {
	var ev core.RouteEvent
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.events = append(s.events, ev)
	// Keep desired state in sync with events.
	switch ev.Type {
	case core.EventRouteCreated, core.EventRouteUpdated:
		if ev.Route.Host != "" {
			s.routes[ev.Route.Host] = ev.Route
		}
	case core.EventRouteDeleted:
		delete(s.routes, ev.Route.Host)
	}
	ch := s.eventC
	s.mu.Unlock()

	// If trigger is running, forward the event.
	if ch != nil {
		select {
		case ch <- ev:
		default:
			s.logger.Warn("event channel full, dropping event", "host", ev.Route.Host)
		}
	}

	s.logger.Info("event received", "type", ev.Type, "host", ev.Route.Host)
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(ev)
}

func (s *HTTPSource) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.events)
}

func (s *HTTPSource) handlePostRoute(w http.ResponseWriter, r *http.Request) {
	var route core.RouteRequest
	if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if route.Host == "" {
		http.Error(w, `"host" is required`, http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.routes[route.Host] = route
	s.mu.Unlock()

	s.logger.Info("route added to desired state", "host", route.Host)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(route)
}

func (s *HTTPSource) handleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	host := r.PathValue("host")
	if host == "" {
		http.Error(w, "host is required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	_, existed := s.routes[host]
	delete(s.routes, host)
	s.mu.Unlock()

	if !existed {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	s.logger.Info("route removed from desired state", "host", host)
	w.WriteHeader(http.StatusNoContent)
}

func (s *HTTPSource) handleGetRoutes(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	routes := make([]core.RouteRequest, 0, len(s.routes))
	for _, r := range s.routes {
		routes = append(routes, r)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(routes)
}
