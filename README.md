# Dynamic Route Provisioner

A pluggable Go framework for automating TLS route provisioning on gateway and proxy appliances. Designed to run inside Kubernetes.

## How it works

```
Trigger → Certificate Issuer → Route Provisioner
              ↑                        ↑
        Reconciler (periodic diff + batch apply)
              ↑
    Leader Election (HA — only one instance active)
```

1. **Trigger** detects a new route is needed (e.g. a document appears in MongoDB)
2. **Certificate Issuer** obtains a TLS certificate for the hostname (e.g. ACME HTTP-01)
3. **Route Provisioner** creates the route on the target gateway (e.g. Netscaler CPX via Nitro API)
4. **Reconciler** periodically compares desired state (source of truth) vs actual state (gateway), computes the diff, and batch-applies it — handles drift and gateway restarts
5. **Leader Election** ensures only one instance processes events and reconciliation at a time (HA with automatic failover)

The **Orchestrator** wires these components, runs the event loop, and manages the reconciliation and leader election lifecycles.

## Project structure

Go workspace monorepo: `core/` for interfaces, `impl/` for implementations, `cmd/` for runnable apps.

```
dynamic-route-provisioner/
├── go.work
├── core/                              # Interfaces + domain model
│   ├── route.go                       # RouteRequest, Certificate, RouteEvent, ...
│   ├── trigger/                       # trigger.Trigger
│   ├── certificate/                   # certificate.Issuer
│   ├── provisioner/                   # provisioner.RouteProvisioner (incl. List, Batch*)
│   ├── desired/                       # desired.DesiredStateProvider
│   ├── reconciler/                    # Reconciler (diff + batch apply)
│   ├── lease/                         # lease.LeaderElector
│   └── orchestrator/                  # Orchestrator (pipeline coordinator)
├── impl/
│   ├── source-mongo/                  # MongoDB change stream trigger + desired state
│   ├── cert-acme-dns/                 # ACME DNS-01 certificate issuer
│   ├── cert-acme-http/                # ACME HTTP-01 certificate issuer
│   ├── cert-selfsigned/               # Self-signed certificate issuer (testing)
│   ├── provisioner-netscaler/         # Netscaler CPX (Nitro API) provisioner
│   ├── lease-mongo/                   # MongoDB-based leader election
│   └── lease-kube/                    # Kubernetes Lease API leader election
└── cmd/
    └── routes-provisioner/               # Concrete use-case application
        ├── main.go                    # Entrypoint
        ├── internal/                  # Config, mappers, challenge server
        └── config.yaml                # Example configuration
```

## Core interfaces

```go
type Trigger interface {
    Start(ctx context.Context, events chan<- core.RouteEvent) error
    Name() string
}

type Issuer interface {
    Issue(ctx context.Context, req core.RouteRequest) (*core.Certificate, error)
    Revoke(ctx context.Context, cert core.Certificate) error
    Name() string
}

type RouteProvisioner interface {
    Provision(ctx, req, cert) (*core.ProvisionedRoute, error)
    Deprovision(ctx, routeID) error
    List(ctx) ([]core.ProvisionedRoute, error)
    BatchProvision(ctx, routes, certs) ([]core.ProvisionedRoute, error)
    BatchDeprovision(ctx, routeIDs) error
    Name() string
}

type DesiredStateProvider interface {
    List(ctx context.Context) ([]core.RouteRequest, error)
    Name() string
}

type LeaderElector interface {
    Run(ctx context.Context, callbacks LeaderCallbacks) error
    IsLeader() bool
    Name() string
}
```

## Available implementations

| Module | Interface | Extension point | Description |
|---|---|---|---|
| `source-mongo` | `Trigger` + `DesiredStateProvider` | `DocumentMapper` | MongoDB change streams + full collection listing for reconciliation |
| `cert-acme-dns` | `Issuer` | `DNSProvider` | ACME DNS-01 challenges (supports wildcards) |
| `cert-acme-http` | `Issuer` | `ChallengeSolver` | ACME HTTP-01 challenges |
| `cert-selfsigned` | `Issuer` | — | Self-signed certificates for testing/development |
| `provisioner-netscaler` | `RouteProvisioner` | `ResourceMapper` | Netscaler CPX via Nitro REST API (incl. batch and list) |
| `lease-mongo` | `LeaderElector` | — | MongoDB document-based leader election with TTL |
| `lease-kube` | `LeaderElector` | — | Kubernetes coordination/v1 Lease API |

## Routes Provisioner (use-case app)

Watches a MongoDB `workspace` collection for documents with a `url` field, issues ACME certificates, and configures a Netscaler CPX gateway. Supports state reconciliation and leader election for HA deployments.

### Configuration

YAML base config with environment variable overrides (via [Viper](https://github.com/spf13/viper)). Env vars use the `ROUTES_` prefix:

```yaml
datasource:
  provider: "mongodb"                    # ROUTES_DATASOURCE_PROVIDER
  uri: "mongodb://localhost:27017"       # ROUTES_DATASOURCE_URI
  database: "mydb"                       # ROUTES_DATASOURCE_DATABASE
  collection: "workspace"               # ROUTES_DATASOURCE_COLLECTION

certificate:
  provider: "selfsigned"                 # ROUTES_CERTIFICATE_PROVIDER — "acme-http", "acme-dns", or "selfsigned"
  email: "admin@example.com"             # ROUTES_CERTIFICATE_EMAIL (acme-http/acme-dns)
  directory_url: "https://acme-..."      # ROUTES_CERTIFICATE_DIRECTORY_URL (acme-http/acme-dns)
  challenge_port: 80                     # ROUTES_CERTIFICATE_CHALLENGE_PORT (acme-http only)
  validity: "8760h"                      # ROUTES_CERTIFICATE_VALIDITY (selfsigned only)
  organization: "Self-Signed"            # ROUTES_CERTIFICATE_ORGANIZATION (selfsigned only)

provisioner:
  provider: "netscaler"                  # ROUTES_PROVISIONER_PROVIDER
  endpoint: "https://10.0.0.1"           # ROUTES_PROVISIONER_ENDPOINT
  username: "nsroot"                     # ROUTES_PROVISIONER_USERNAME
  password: "secret"                     # ROUTES_PROVISIONER_PASSWORD
  insecure_skip_verify: true             # ROUTES_PROVISIONER_INSECURE_SKIP_VERIFY

reconcile:
  interval: "5m"                         # ROUTES_RECONCILE_INTERVAL

leader_election:
  enabled: false                         # ROUTES_LEADER_ELECTION_ENABLED
  provider: "kube"                       # ROUTES_LEADER_ELECTION_PROVIDER — "kube" or "mongo"
  lease_name: "routes-provisioner-leader"   # ROUTES_LEADER_ELECTION_LEASE_NAME
  namespace: "default"                   # ROUTES_LEADER_ELECTION_NAMESPACE (kube only)
  lease_duration: "15s"                  # ROUTES_LEADER_ELECTION_LEASE_DURATION
  renew_deadline: "10s"                  # ROUTES_LEADER_ELECTION_RENEW_DEADLINE (kube only)
  retry_interval: "2s"                   # ROUTES_LEADER_ELECTION_RETRY_INTERVAL
```

### Run

```bash
cd cmd/routes-provisioner
go build -o routes-provisioner .
./routes-provisioner -config config.yaml

# Or via env vars
ROUTES_CONFIG_PATH=/etc/routes/config.yaml ./routes-provisioner
```

## Building

Requires Go 1.25.4+.

```bash
# Build a module
cd cmd/routes-provisioner && go build ./...

# Sync workspace after changes
go work sync
```

## Adding a new implementation

1. Create `impl/<type>-<name>/` and init a Go module
2. Add to `go.work` use block
3. Implement the core interface with an extension interface for customization
4. `go work sync && go build ./...`

## License

TBD
