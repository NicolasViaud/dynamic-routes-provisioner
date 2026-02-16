package certacmehttp

import "context"

// ChallengeSolver is the abstraction that developers implement to control how
// the ACME HTTP-01 challenge token is served. The token must be reachable at:
//
//	http://<domain>/.well-known/acme-challenge/<token>
//
// Possible implementations: Kubernetes Ingress annotation, standalone HTTP
// server, shared volume, cloud load-balancer rule, etc.
type ChallengeSolver interface {
	// Present makes the key authorization response reachable at the HTTP-01
	// challenge path for the given domain. It must block until the response
	// is ready to be served.
	Present(ctx context.Context, domain, token, keyAuth string) error

	// Cleanup removes the challenge response after validation succeeds or
	// fails. It is always called, even if validation failed.
	Cleanup(ctx context.Context, domain, token string) error
}
