package provnetscaler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Action represents a Nitro API operation type.
type Action string

const (
	ActionAdd    Action = "add"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
	ActionBind   Action = "bind"
	ActionUnbind Action = "unbind"
)

// NitroResource describes a single Netscaler configuration resource.
type NitroResource struct {
	Type       string         // resource type: "lbvserver", "csvserver", "sslcertkey", "service", etc.
	Name       string         // resource name
	Properties map[string]any // resource-specific fields sent to the Nitro API
}

// NitroOperation is an ordered instruction to execute against the Nitro API.
type NitroOperation struct {
	Action   Action
	Resource NitroResource
}

// NitroClient handles authenticated HTTP communication with the Netscaler
// Nitro REST API at /nitro/v1/config/.
type NitroClient struct {
	endpoint   string // e.g. "https://10.0.0.1"
	username   string
	password   string
	httpClient *http.Client
}

// NewNitroClient creates a NitroClient for the given endpoint and credentials.
func NewNitroClient(endpoint, username, password string, httpClient *http.Client) *NitroClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &NitroClient{
		endpoint:   endpoint,
		username:   username,
		password:   password,
		httpClient: httpClient,
	}
}

// Execute runs a single NitroOperation against the API.
func (c *NitroClient) Execute(ctx context.Context, op NitroOperation) error {
	switch op.Action {
	case ActionAdd:
		return c.doPost(ctx, op.Resource)
	case ActionUpdate:
		return c.doPut(ctx, op.Resource)
	case ActionDelete:
		return c.doDelete(ctx, op.Resource)
	case ActionBind, ActionUnbind:
		return c.doPost(ctx, op.Resource)
	default:
		return fmt.Errorf("unknown action: %s", op.Action)
	}
}

func (c *NitroClient) doPost(ctx context.Context, res NitroResource) error {
	body := c.buildPayload(res)
	url := fmt.Sprintf("%s/nitro/v1/config/%s", c.endpoint, res.Type)
	return c.do(ctx, http.MethodPost, url, body)
}

func (c *NitroClient) doPut(ctx context.Context, res NitroResource) error {
	body := c.buildPayload(res)
	url := fmt.Sprintf("%s/nitro/v1/config/%s", c.endpoint, res.Type)
	return c.do(ctx, http.MethodPut, url, body)
}

func (c *NitroClient) doDelete(ctx context.Context, res NitroResource) error {
	url := fmt.Sprintf("%s/nitro/v1/config/%s/%s", c.endpoint, res.Type, res.Name)
	return c.do(ctx, http.MethodDelete, url, nil)
}

func (c *NitroClient) buildPayload(res NitroResource) map[string]any {
	props := make(map[string]any, len(res.Properties)+1)
	props["name"] = res.Name
	for k, v := range res.Properties {
		props[k] = v
	}
	return map[string]any{
		res.Type: props,
	}
}

func (c *NitroClient) do(ctx context.Context, method, url string, body any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(c.username, c.password)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nitro request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return &NitroError{
			StatusCode: resp.StatusCode,
			Method:     method,
			URL:        url,
			Body:       string(respBody),
		}
	}

	return nil
}

// NitroError represents an error response from the Nitro API.
type NitroError struct {
	StatusCode int
	Method     string
	URL        string
	Body       string
}

func (e *NitroError) Error() string {
	return fmt.Sprintf("nitro %s %s returned %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}
