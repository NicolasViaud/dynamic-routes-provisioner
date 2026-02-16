# Dynamic Route Provisioner

A pluggable Go framework for automating TLS route provisioning on gateway and proxy appliances. Designed to run inside Kubernetes.

## Why Dynamic Route Provisioner?

The traditional Kubernetes approach — cert-manager + HTTPRoute CRDs + an ingress controller — routes every change through the Kubernetes API server and etcd:

```
App DB change → create HTTPRoute CR  → etcd write → controller watch
             → create Certificate CR → etcd write → cert-manager watch → issue cert → write Secret → etcd write
             → ingress controller watches Secret + HTTPRoute → configure gateway
```

For **N routes**, this creates at least **3N Kubernetes objects** (HTTPRoute + Certificate + Secret), each written to etcd and watched by multiple controllers. Two independent reconciliation loops (cert-manager and the ingress controller) coordinate implicitly by waiting on each other's resources.

Dynamic Route Provisioner replaces this with a **single direct pipeline**:

```
MongoDB change stream → Orchestrator → issue cert → provision gateway
```

### No etcd bottleneck

etcd has hard throughput limits. Every route in the traditional stack generates multiple etcd writes and watch notifications fanning out to controllers. This project bypasses the Kubernetes API server entirely — the source of truth is your application database (e.g. MongoDB), which you already scale independently. Leader election uses MongoDB TTL documents or Kubernetes Lease API directly, with no etcd coordination overhead.

### No API server pressure

The traditional stack puts constant load on the Kubernetes API server: cert-manager and the ingress controller each maintain long-lived watches, list resources on resync, and write back status updates. At scale, this competes with every other controller and operator in the cluster for API server capacity. Dynamic Route Provisioner talks directly to MongoDB and the gateway API — the Kubernetes API server is not in the hot path at all, freeing up cluster resources for workloads that actually need them.

### Single pipeline, no controller coordination

cert-manager and the ingress controller are two separate reconciliation loops. The ingress controller must wait for cert-manager to write a Secret before it can act — added latency on every route. Here, certificate issuance and gateway provisioning happen sequentially in one process: change stream event → cert issued → route provisioned. No intermediate CRs, no watch propagation delay, no polling for another controller's output. Drift correction uses `BatchProvision` / `BatchDeprovision` to fix multiple routes in a single gateway API call, instead of per-resource reconciliation.

### Pluggable certificate storage

cert-manager is hardwired to Kubernetes Secrets — private keys and certificates always land in etcd, and you're limited to whatever encryption-at-rest Kubernetes provides. Dynamic Route Provisioner treats certificate storage as a pluggable concern. The certificate store is a decorator that wraps any issuer and can cache certificates in Kubernetes Secrets, HashiCorp Vault KV v2, or any other backend you implement. This means you can store private keys in a proper secrets manager with audit logging, access policies, and automatic rotation — without changing a single line of issuer or provisioner code.

## How it works

```
Trigger → Certificate Store (cache) → Certificate Issuer → Route Provisioner
                   ↑                          ↑                     ↑
             Reconciler (periodic diff + batch apply)
                   ↑
         Leader Election (HA — only one instance active)
```

1. **Trigger** detects a new route is needed (e.g. a document appears in MongoDB)
2. **Certificate Store** checks for a cached, still-valid certificate (K8s Secrets or Vault KV v2) — avoids unnecessary reissuance after gateway restarts or ACME rate limits
3. **Certificate Issuer** obtains a TLS certificate for the hostname if not cached (e.g. ACME HTTP-01, Vault PKI)
4. **Route Provisioner** creates the route on the target gateway (e.g. Netscaler CPX via Nitro API)
5. **Reconciler** periodically compares desired state (source of truth) vs actual state (gateway), computes the diff, and batch-applies it — handles drift and gateway restarts
6. **Leader Election** ensures only one instance processes events and reconciliation at a time (HA with automatic failover)

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
│   ├── cert-vault/                    # HashiCorp Vault PKI certificate issuer
│   ├── certstore-kube/                # Certificate store — Kubernetes TLS Secrets
│   ├── certstore-vault/               # Certificate store — Vault KV v2
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
| `cert-vault` | `Issuer` | — | HashiCorp Vault PKI secrets engine (role-based issuance) |
| `certstore-kube` | `Issuer` (decorator) | — | Caches certificates as Kubernetes TLS Secrets |
| `certstore-vault` | `Issuer` (decorator) | — | Caches certificates in Vault KV v2 |
| `provisioner-netscaler` | `RouteProvisioner` | `ResourceMapper` | Netscaler CPX via Nitro REST API (incl. batch and list) |
| `lease-mongo` | `LeaderElector` | — | MongoDB document-based leader election with TTL |
| `lease-kube` | `LeaderElector` | — | Kubernetes coordination/v1 Lease API |

## Routes Provisioner (use-case app)

Watches a MongoDB collection for documents with a `url` field, issues ACME certificates, and configures a Netscaler CPX gateway. Supports state reconciliation and leader election for HA deployments.

### Configuration

YAML base config with environment variable overrides (via [Viper](https://github.com/spf13/viper)). Env vars use the `ROUTES_` prefix:

```yaml
datasource:
  provider: "mongodb"                    # ROUTES_DATASOURCE_PROVIDER
  uri: "mongodb://localhost:27017"       # ROUTES_DATASOURCE_URI
  database: "mydb"                       # ROUTES_DATASOURCE_DATABASE
  collection: "routes"               # ROUTES_DATASOURCE_COLLECTION

certificate:
  provider: "selfsigned"                 # ROUTES_CERTIFICATE_PROVIDER — "acme-http", "acme-dns", "selfsigned", or "vault"
  email: "admin@example.com"             # ROUTES_CERTIFICATE_EMAIL (acme-http/acme-dns)
  directory_url: "https://acme-..."      # ROUTES_CERTIFICATE_DIRECTORY_URL (acme-http/acme-dns)
  challenge_port: 80                     # ROUTES_CERTIFICATE_CHALLENGE_PORT (acme-http only)
  validity: "8760h"                      # ROUTES_CERTIFICATE_VALIDITY (selfsigned only)
  organization: "Self-Signed"            # ROUTES_CERTIFICATE_ORGANIZATION (selfsigned only)
  # vault_address: "http://vault:8200"   # ROUTES_CERTIFICATE_VAULT_ADDRESS (vault only)
  # vault_role: "my-role"                # ROUTES_CERTIFICATE_VAULT_ROLE (vault only)

certificate_store:
  enabled: false                         # ROUTES_CERTIFICATE_STORE_ENABLED
  provider: "kube"                       # ROUTES_CERTIFICATE_STORE_PROVIDER — "kube" or "vault"
  namespace: "default"                   # ROUTES_CERTIFICATE_STORE_NAMESPACE (kube only)
  secret_prefix: "route-tls"            # ROUTES_CERTIFICATE_STORE_SECRET_PREFIX (kube only)
  renew_before: "720h"                   # ROUTES_CERTIFICATE_STORE_RENEW_BEFORE — re-issue 30 days before expiry

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
