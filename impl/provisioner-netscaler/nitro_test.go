package provnetscaler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(handler http.HandlerFunc) (*httptest.Server, *NitroClient) {
	srv := httptest.NewServer(handler)
	client := NewNitroClient(srv.URL, "admin", "secret", srv.Client())
	return srv, client
}

func TestExecute_Add_SendsPostWithPayload(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotBody map[string]any

	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	err := client.Execute(context.Background(), NitroOperation{
		Action: ActionAdd,
		Resource: NitroResource{
			Type:       "lbvserver",
			Name:       "vs-test",
			Properties: map[string]any{"servicetype": "SSL", "ipv46": "10.0.0.1"},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/nitro/v1/config/lbvserver" {
		t.Errorf("unexpected path: %s", gotPath)
	}
	if gotAuth == "" {
		t.Error("expected Authorization header to be set")
	}

	res, ok := gotBody["lbvserver"].(map[string]any)
	if !ok {
		t.Fatal("payload missing 'lbvserver' key")
	}
	if res["name"] != "vs-test" {
		t.Errorf("expected name vs-test, got %v", res["name"])
	}
	if res["servicetype"] != "SSL" {
		t.Errorf("expected servicetype SSL, got %v", res["servicetype"])
	}
}

func TestExecute_Update_SendsPut(t *testing.T) {
	var gotMethod string

	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	err := client.Execute(context.Background(), NitroOperation{
		Action:   ActionUpdate,
		Resource: NitroResource{Type: "lbvserver", Name: "vs-test"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("expected PUT, got %s", gotMethod)
	}
}

func TestExecute_Delete_SendsDeleteWithName(t *testing.T) {
	var gotMethod, gotPath string

	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	err := client.Execute(context.Background(), NitroOperation{
		Action:   ActionDelete,
		Resource: NitroResource{Type: "sslcertkey", Name: "cert-app"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", gotMethod)
	}
	if gotPath != "/nitro/v1/config/sslcertkey/cert-app" {
		t.Errorf("unexpected path: %s", gotPath)
	}
}

func TestExecute_Bind_SendsPost(t *testing.T) {
	var gotMethod string

	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	err := client.Execute(context.Background(), NitroOperation{
		Action:   ActionBind,
		Resource: NitroResource{Type: "sslvserver_sslcertkey_binding", Name: "vs-test"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("expected POST for bind, got %s", gotMethod)
	}
}

func TestExecute_UnknownAction_ReturnsError(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	err := client.Execute(context.Background(), NitroOperation{
		Action:   Action("invalid"),
		Resource: NitroResource{Type: "lbvserver", Name: "vs-test"},
	})

	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestExecute_NitroError(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"errorcode":273,"message":"Resource already exists"}`))
	})
	defer srv.Close()

	err := client.Execute(context.Background(), NitroOperation{
		Action:   ActionAdd,
		Resource: NitroResource{Type: "lbvserver", Name: "vs-test"},
	})

	nitroErr, ok := err.(*NitroError)
	if !ok {
		t.Fatalf("expected *NitroError, got %T: %v", err, err)
	}
	if nitroErr.StatusCode != http.StatusConflict {
		t.Errorf("expected 409, got %d", nitroErr.StatusCode)
	}
	if nitroErr.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", nitroErr.Method)
	}
}

func TestList_ReturnsResources(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		resp := map[string]any{
			"csvserver": []map[string]any{
				{"name": "vs-app1", "servicetype": "SSL"},
				{"name": "vs-app2", "servicetype": "HTTP"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})
	defer srv.Close()

	resources, err := client.List(context.Background(), "csvserver", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}
	if resources[0]["name"] != "vs-app1" {
		t.Errorf("expected vs-app1, got %v", resources[0]["name"])
	}
}

func TestList_WithFilter(t *testing.T) {
	var gotQuery string

	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode(map[string]any{"csvserver": []map[string]any{}})
	})
	defer srv.Close()

	client.List(context.Background(), "csvserver", "name:routes-*")
	if gotQuery != "filter=name:routes-*" {
		t.Errorf("unexpected query: %s", gotQuery)
	}
}

func TestList_EmptyResult(t *testing.T) {
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{})
	})
	defer srv.Close()

	resources, err := client.List(context.Background(), "csvserver", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resources != nil {
		t.Errorf("expected nil, got %v", resources)
	}
}

func TestBatchExecute_SendsBatchPayload(t *testing.T) {
	var gotBody map[string]any

	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	ops := []NitroOperation{
		{Action: ActionAdd, Resource: NitroResource{Type: "lbvserver", Name: "vs-1"}},
		{Action: ActionAdd, Resource: NitroResource{Type: "service", Name: "svc-1"}},
	}

	err := client.BatchExecute(context.Background(), ops)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	params, ok := gotBody["params"].(map[string]any)
	if !ok {
		t.Fatal("missing params in batch request")
	}
	if params["action"] != "batch" {
		t.Errorf("expected batch action, got %v", params["action"])
	}

	batch, ok := gotBody["batch"].([]any)
	if !ok {
		t.Fatal("missing batch array in request")
	}
	if len(batch) != 2 {
		t.Errorf("expected 2 batch operations, got %d", len(batch))
	}
}

func TestBatchExecute_EmptyOps(t *testing.T) {
	called := false
	srv, client := newTestServer(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	err := client.BatchExecute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("expected no HTTP call for empty ops")
	}
}
