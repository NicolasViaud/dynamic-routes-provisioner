# Dynamic Route Provisioner

A pluggable Go framework for automating TLS route provisioning on gateway and proxy appliances. Designed to run inside Kubernetes.

## How it works

```
Trigger → Certificate Issuer → Route Provisioner
```

1. **Trigger** detects a new route is needed (e.g. a document appears in MongoDB)
2. **Certificate Issuer** obtains a TLS certificate for the hostname (e.g. ACME HTTP-01)
3. **Route Provisioner** creates the route on the target gateway (e.g. Netscaler CPX via Nitro API)

The **Orchestrator** wires these three components and runs the event loop.

## Project structure

Go workspace monorepo: `core/` for interfaces, `impl/` for implementations, `cmd/` for runnable apps.

```
dynamic-route-provisioner/
├── go.work
├── core/                              # Interfaces + domain model
│   ├── route.go                       # RouteRequest, Certificate, RouteEvent, ...
│   ├── trigger/                       # trigger.Trigger
│   ├── certificate/                   # certificate.Issuer
│   ├── provisioner/                   # provisioner.RouteProvisioner
│   └── orchestrator/                  # Orchestrator (pipeline coordinator)
├── impl/
│   ├── trigger-mongo/                 # MongoDB change stream trigger
│   ├── cert-acme-http/                # ACME HTTP-01 certificate issuer
│   └── provisioner-netscaler/         # Netscaler CPX (Nitro API) provisioner
└── cmd/
    └── sds-provisioner/               # Concrete use-case application
        ├── cmd/sds-provisioner/        # Entrypoint
        ├── internal/                   # Config, mappers, challenge server
        └── config.yaml                 # Example configuration
```

## Core interfaces

```go
// Detects route changes from an external source.
type Trigger interface {
    Start(ctx context.Context, events chan<- core.RouteEvent) error
    Name() string
}

// Obtains TLS certificates.
type Issuer interface {
    Issue(ctx context.Context, req core.RouteRequest) (*core.Certificate, error)
    Revoke(ctx context.Context, cert core.Certificate) error
    Name() string
}

// Creates/removes routes on a gateway.
type RouteProvisioner interface {
    Provision(ctx context.Context, req core.RouteRequest, cert *core.Certificate) (*core.ProvisionedRoute, error)
    Deprovision(ctx context.Context, routeID string) error
    Name() string
}
```

## Available implementations

| Module | Interface | Extension point | Description |
|---|---|---|---|
| `trigger-mongo` | `Trigger` | `DocumentMapper` | MongoDB change streams. Developer defines which documents to watch and how to extract route info. |
| `cert-acme-http` | `Issuer` | `ChallengeSolver` | ACME HTTP-01 challenges. Developer controls how the challenge token is served. |
| `provisioner-netscaler` | `RouteProvisioner` | `ResourceMapper` | Netscaler CPX via Nitro REST API. Developer defines which Nitro resources to create. |

## SDS Provisioner (use-case app)

Watches a MongoDB `workspace` collection for documents with a `url` field, issues ACME certificates, and configures a Netscaler CPX gateway.

### Configuration

YAML base config with environment variable overrides (via [Viper](https://github.com/spf13/viper)). Env vars use the `SDS_` prefix:

```yaml
mongodb:
  uri: "mongodb://localhost:27017"       # SDS_MONGODB_URI
  database: "mydb"                       # SDS_MONGODB_DATABASE
  collection: "workspace"                # SDS_MONGODB_COLLECTION

acme:
  email: "admin@example.com"             # SDS_ACME_EMAIL
  directory_url: "https://acme-..."      # SDS_ACME_DIRECTORY_URL
  challenge_port: 80                     # SDS_ACME_CHALLENGE_PORT

netscaler:
  endpoint: "https://10.0.0.1"           # SDS_NETSCALER_ENDPOINT
  username: "nsroot"                     # SDS_NETSCALER_USERNAME
  password: "secret"                     # SDS_NETSCALER_PASSWORD
  insecure_skip_verify: true             # SDS_NETSCALER_INSECURE_SKIP_VERIFY
```

### Run

```bash
cd cmd/sds-provisioner
go build -o sds-provisioner ./cmd/sds-provisioner
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
