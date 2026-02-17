package sourcehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	core "github.com/nicol/dynamic-route-provisioner/core"
)

func setup() (*HTTPSource, *httptest.Server) {
	src := New(nil)
	srv := httptest.NewServer(src.Handler())
	return src, srv
}

func TestPostAndGetRoutes(t *testing.T) {
	_, srv := setup()
	defer srv.Close()

	route := core.RouteRequest{
		Host: "app.example.com",
		Path: "/",
		TLS:  true,
		Backends: []core.Backend{
			{ServiceName: "app-svc", Port: 8080, Weight: 100},
		},
	}

	body, _ := json.Marshal(route)
	resp, err := http.Post(srv.URL+"/routes", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /routes failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// GET /routes should return the route.
	resp, err = http.Get(srv.URL + "/routes")
	if err != nil {
		t.Fatalf("GET /routes failed: %v", err)
	}
	defer resp.Body.Close()

	var routes []core.RouteRequest
	json.NewDecoder(resp.Body).Decode(&routes)

	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if routes[0].Host != "app.example.com" {
		t.Errorf("expected app.example.com, got %s", routes[0].Host)
	}
	if !routes[0].TLS {
		t.Error("expected TLS to be true")
	}
}

func TestPostRoute_MissingHost(t *testing.T) {
	_, srv := setup()
	defer srv.Close()

	body, _ := json.Marshal(core.RouteRequest{Path: "/"})
	resp, err := http.Post(srv.URL+"/routes", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /routes failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestDeleteRoute(t *testing.T) {
	_, srv := setup()
	defer srv.Close()

	// Add a route.
	body, _ := json.Marshal(core.RouteRequest{Host: "app.example.com"})
	http.Post(srv.URL+"/routes", "application/json", bytes.NewReader(body))

	// Delete it.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/routes/app.example.com", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE /routes failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}

	// GET should return empty.
	resp, _ = http.Get(srv.URL + "/routes")
	var routes []core.RouteRequest
	json.NewDecoder(resp.Body).Decode(&routes)
	resp.Body.Close()

	if len(routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(routes))
	}
}

func TestDeleteRoute_NotFound(t *testing.T) {
	_, srv := setup()
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/routes/nonexistent", nil)
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestPostEvent_AndGetEvents(t *testing.T) {
	_, srv := setup()
	defer srv.Close()

	ev := core.RouteEvent{
		Type: core.EventRouteCreated,
		Route: core.RouteRequest{
			Host: "new.example.com",
			TLS:  true,
		},
	}

	body, _ := json.Marshal(ev)
	resp, err := http.Post(srv.URL+"/events", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /events failed: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// GET /events should return it.
	resp, _ = http.Get(srv.URL + "/events")
	var events []core.RouteEvent
	json.NewDecoder(resp.Body).Decode(&events)
	resp.Body.Close()

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != core.EventRouteCreated {
		t.Errorf("expected route.created, got %s", events[0].Type)
	}
}

func TestTrigger_ForwardsEvents(t *testing.T) {
	src, srv := setup()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events := make(chan core.RouteEvent, 1)
	go src.Start(ctx, events)

	// Give Start a moment to initialize the channel.
	time.Sleep(50 * time.Millisecond)

	// POST an event via HTTP.
	ev := core.RouteEvent{
		Type:  core.EventRouteCreated,
		Route: core.RouteRequest{Host: "trigger.example.com"},
	}
	body, _ := json.Marshal(ev)
	http.Post(srv.URL+"/events", "application/json", bytes.NewReader(body))

	// Should appear on the trigger channel.
	select {
	case got := <-events:
		if got.Route.Host != "trigger.example.com" {
			t.Errorf("expected trigger.example.com, got %s", got.Route.Host)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event on trigger channel")
	}
}

func TestDesiredStateProvider_List(t *testing.T) {
	src, srv := setup()
	defer srv.Close()

	// Add two routes via HTTP.
	for _, host := range []string{"a.example.com", "b.example.com"} {
		body, _ := json.Marshal(core.RouteRequest{Host: host})
		http.Post(srv.URL+"/routes", "application/json", bytes.NewReader(body))
	}

	// Call List directly.
	routes, err := src.List(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(routes))
	}
}
