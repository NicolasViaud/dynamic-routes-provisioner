package certacmedns

import "context"

// DNSProvider is the abstraction that developers implement to control how
// the ACME DNS-01 challenge TXT record is created. The record must be set at:
//
//	_acme-challenge.<domain>  TXT  <value>
//
// Possible implementations: Cloudflare API, AWS Route53, Google Cloud DNS,
// Azure DNS, manual scripts, etc.
type DNSProvider interface {
	// Present creates a TXT record at _acme-challenge.<domain> with the
	// given value. It must block until the record is propagated and
	// resolvable.
	Present(ctx context.Context, domain, value string) error

	// Cleanup removes the TXT record after validation succeeds or fails.
	// It is always called, even if validation failed.
	Cleanup(ctx context.Context, domain string) error
}
