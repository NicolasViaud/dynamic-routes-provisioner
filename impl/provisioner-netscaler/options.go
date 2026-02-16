package provnetscaler

import (
	"crypto/tls"
	"net/http"
)

// Option configures a NetscalerProvisioner.
type Option func(*config)

type config struct {
	endpoint   string
	username   string
	password   string
	httpClient *http.Client
}

// WithEndpoint sets the Netscaler management endpoint (e.g. "https://10.0.0.1").
func WithEndpoint(url string) Option {
	return func(c *config) {
		c.endpoint = url
	}
}

// WithCredentials sets the Nitro API username and password.
func WithCredentials(username, password string) Option {
	return func(c *config) {
		c.username = username
		c.password = password
	}
}

// WithHTTPClient sets a custom HTTP client (e.g. for timeouts or mTLS).
func WithHTTPClient(client *http.Client) Option {
	return func(c *config) {
		c.httpClient = client
	}
}

// WithInsecureSkipVerify creates an HTTP client that skips TLS certificate
// verification. Useful for CPX instances with self-signed certificates.
func WithInsecureSkipVerify() Option {
	return func(c *config) {
		c.httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}
	}
}
