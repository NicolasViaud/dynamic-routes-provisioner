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
│   ├── cert-acme-http/                # ACME HTTP-01 certificate issuer
│   ├── cert-selfsigned/               # Self-signed certificate issuer (testing)
│   ├── provisioner-netscaler/         # Netscaler CPX (Nitro API) provisioner
│   ├── lease-mongo/                   # MongoDB-based leader election
│   └── lease-kube/                    # Kubernetes Lease API leader election
└── cmd/
    └── sds-provisioner/               # Concrete use-case application
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
| `cert-acme-http` | `Issuer` | `ChallengeSolver` | ACME HTTP-01 challenges |
| `cert-selfsigned` | `Issuer` | — | Self-signed certificates for testing/development |
| `provisioner-netscaler` | `RouteProvisioner` | `ResourceMapper` | Netscaler CPX via Nitro REST API (incl. batch and list) |
| `lease-mongo` | `LeaderElector` | — | MongoDB document-based leader election with TTL |
| `lease-kube` | `LeaderElector` | — | Kubernetes coordination/v1 Lease API |

## SDS Provisioner (use-case app)

Watches a MongoDB `workspace` collection for documents with a `url` field, issues ACME certificates, and configures a Netscaler CPX gateway. Supports state reconciliation and leader election for HA deployments.

### Configuration

YAML base config with environment variable overrides (via [Viper](https://github.com/spf13/viper)). Env vars use the `SDS_` prefix:

```yaml
datasource:
  provider: "mongodb"                    # SDS_DATASOURCE_PROVIDER
  uri: "mongodb://localhost:27017"       # SDS_DATASOURCE_URI
  database: "mydb"                       # SDS_DATASOURCE_DATABASE
  collection: "workspace"               # SDS_DATASOURCE_COLLECTION

certificate:
  provider: "selfsigned"                 # SDS_CERTIFICATE_PROVIDER — "acme" or "selfsigned"
  email: "admin@example.com"             # SDS_CERTIFICATE_EMAIL (acme only)
  directory_url: "https://acme-..."      # SDS_CERTIFICATE_DIRECTORY_URL (acme only)
  challenge_port: 80                     # SDS_CERTIFICATE_CHALLENGE_PORT (acme only)
  validity: "8760h"                      # SDS_CERTIFICATE_VALIDITY (selfsigned only)
  organization: "Self-Signed"            # SDS_CERTIFICATE_ORGANIZATION (selfsigned only)

provisioner:
  provider: "netscaler"                  # SDS_PROVISIONER_PROVIDER
  endpoint: "https://10.0.0.1"           # SDS_PROVISIONER_ENDPOINT
  username: "nsroot"                     # SDS_PROVISIONER_USERNAME
  password: "secret"                     # SDS_PROVISIONER_PASSWORD
  insecure_skip_verify: true             # SDS_PROVISIONER_INSECURE_SKIP_VERIFY

reconcile:
  interval: "5m"                         # SDS_RECONCILE_INTERVAL

leader_election:
  enabled: false                         # SDS_LEADER_ELECTION_ENABLED
  provider: "kube"                       # SDS_LEADER_ELECTION_PROVIDER — "kube" or "mongo"
  lease_name: "sds-provisioner-leader"   # SDS_LEADER_ELECTION_LEASE_NAME
  namespace: "default"                   # SDS_LEADER_ELECTION_NAMESPACE (kube only)
  lease_duration: "15s"                  # SDS_LEADER_ELECTION_LEASE_DURATION
  renew_deadline: "10s"                  # SDS_LEADER_ELECTION_RENEW_DEADLINE (kube only)
  retry_interval: "2s"                   # SDS_LEADER_ELECTION_RETRY_INTERVAL
```

### Run

```bash
cd cmd/sds-provisioner
go build -o sds-provisioner .
./sds-provisioner -config config.yaml

# Or via env vars
SDS_CONFIG_PATH=/etc/sds/config.yaml ./sds-provisioner
```

## Building

Requires Go 1.25.4+.

```bash
# Build a module
cd cmd/sds-provisioner && go build ./...

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
