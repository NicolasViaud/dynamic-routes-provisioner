# Dynamic Route Provisioner

## Why Dynamic Route Provisioner?

The traditional Kubernetes routing stack (cert-manager + HTTPRoute/Ingress + ingress controller) creates at least **3N objects** for N routes, each written to etcd and watched by multiple controllers. This works for static configurations but becomes a bottleneck when routes are dynamic — driven by user actions, tenant onboarding, or application data.

Dynamic Route Provisioner replaces this with a **single direct pipeline**: a change in the application database triggers certificate issuance and gateway provisioning in one process, with no intermediate custom resources.

### No etcd bottleneck

etcd has hard throughput limits. Every route in the traditional stack generates multiple etcd writes and watch notifications fanning out to controllers. This project can bypass the Kubernetes API server entirely — the source of truth is your application database (e.g. MongoDB), which you already scale independently.

### No API server pressure

The traditional stack puts constant load on the Kubernetes API server: cert-manager and the ingress controller each maintain long-lived watches, list resources on resync, and write back status updates. At scale, this competes with every other controller and operator in the cluster for API server capacity. Dynamic Route Provisioner can talk directly to MongoDB and the gateway API — the Kubernetes API server is not in the hot path at all, freeing up cluster resources for workloads that actually need them.

### Single pipeline, no controller coordination

cert-manager and the ingress controller are two separate reconciliation loops. The ingress controller must wait for cert-manager to write a Secret before it can act — added latency on every route. Here, certificate issuance and gateway provisioning happen sequentially in one process: change stream event, cert issued, route provisioned. No intermediate CRs, no watch propagation delay, no polling for another controller's output. Drift correction uses batch operations to fix multiple routes in a single gateway API call, instead of per-resource reconciliation.

### Pluggable certificate storage

cert-manager is hardwired to Kubernetes Secrets — private keys and certificates always land in etcd, and you're limited to whatever encryption-at-rest Kubernetes provides. Dynamic Route Provisioner treats certificate storage as a pluggable concern. The certificate store is a decorator that wraps any issuer and can cache certificates in Kubernetes Secrets, HashiCorp Vault KV v2, or any other backend you implement. This means you can store private keys in a proper secrets manager with audit logging, access policies, and automatic rotation — without changing a single line of issuer or provisioner code.

## How it works

1. **Trigger** detects a new route is needed (e.g. a document appears in MongoDB)
2. **Certificate Store** checks for a cached, still-valid certificate (K8s Secrets or Vault KV v2) — avoids unnecessary reissuance after gateway restarts or ACME rate limits
3. **Certificate Issuer** obtains a TLS certificate for the hostname if not cached (e.g. ACME HTTP-01, Vault PKI)
4. **Route Provisioner** creates the route on the target gateway (e.g. Netscaler CPX via Nitro API, Kubernetes Ingress)
5. **Reconciler** periodically compares desired state (source of truth) vs actual state (gateway), computes the diff, and batch-applies it — handles drift and gateway restarts
6. **Leader Election** ensures only one instance processes events and reconciliation at a time (HA with automatic failover)

The **Orchestrator** wires these components, runs the event loop, and manages the reconciliation and leader election lifecycles.

## Project structure

Go workspace monorepo: `core/` for interfaces, `impl/` for implementations, `cmd/` for runnable apps.

| Directory | Purpose |
|---|---|
| `core/` | Interfaces and domain model — no external dependencies |
| `core/route.go` | `RouteRequest`, `Certificate`, `RouteEvent`, ... |
| `core/trigger/` | `Trigger` interface + `CompositeTrigger` (fans-in multiple triggers) |
| `core/certificate/` | `Issuer` interface |
| `core/provisioner/` | `RouteProvisioner` interface (incl. `List`, `BatchProvision`, `BatchDeprovision`) |
| `core/desired/` | `DesiredStateProvider` interface + `CompositeDesiredState` (merges multiple providers) |
| `core/reconciler/` | Reconciler — compares desired vs actual, batch-applies diff |
| `core/lease/` | `LeaderElector` interface |
| `core/orchestrator/` | Orchestrator — pipeline coordinator |
| `impl/` | Implementation modules (each its own Go module) |
| `cmd/routes-provisioner/` | Concrete use-case application with Viper-based config |

## Available implementations

| Module | Interface | Extension point | Description |
|---|---|---|---|
| `source-mongo` | `Trigger` + `DesiredStateProvider` | `DocumentMapper` | MongoDB change streams + full collection listing for reconciliation |
| `source-http` | `Trigger` + `DesiredStateProvider` | — | HTTP API trigger with Swagger documentation |
| `cert-acme-dns` | `Issuer` | `DNSProvider` | ACME DNS-01 challenges (supports wildcards) |
| `cert-acme-http` | `Issuer` | `ChallengeSolver` | ACME HTTP-01 challenges |
| `cert-selfsigned` | `Issuer` | — | Self-signed certificates for testing/development |
| `cert-vault` | `Issuer` | — | HashiCorp Vault PKI secrets engine (role-based issuance) |
| `certstore-kube` | `Issuer` (decorator) | — | Caches certificates as Kubernetes TLS Secrets |
| `certstore-vault` | `Issuer` (decorator) | — | Caches certificates in Vault KV v2 |
| `certstore-file` | `Issuer` (decorator) | — | Caches certificates on the local filesystem |
| `provisioner-netscaler` | `RouteProvisioner` | `ResourceMapper` | Netscaler CPX via Nitro REST API (incl. batch and list) |
| `provisioner-ingress` | `RouteProvisioner` | `IngressMapper` | Kubernetes Ingress resources with configurable packing |
| `lease-mongo` | `LeaderElector` | — | MongoDB document-based leader election with TTL |
| `lease-kube` | `LeaderElector` | — | Kubernetes coordination/v1 Lease API |

## Use cases

### Maximum scalability — bypass the Kubernetes API entirely

Best for environments where the number of routes is large and the Kubernetes API server / etcd should not be in the hot path. The application database (MongoDB) is the single source of truth, certificates are issued by Vault PKI and cached in Vault KV v2, and routes are provisioned directly on a Netscaler CPX via its Nitro API.

**Pipeline:** MongoDB change stream &rarr; Vault PKI issuer &rarr; Vault KV v2 cache &rarr; Netscaler Nitro API

No Kubernetes objects are created or watched during normal operation. Leader election can use MongoDB-based leases to stay off the API server entirely. This configuration scales independently of cluster size and avoids etcd write amplification.

Multiple collections are supported — each collection gets its own mapper and backends, and the triggers and desired state providers are automatically composed into a single pipeline.

**Example configuration:**

```yaml
datasource:
  provider: "mongodb"
  uri: "mongodb://mongo:27017"
  database: "mydb"
  collections:
    - collection: "websites"
      mapper:
        url_field: "url"
        path: "/"
        tls: true
        backends:
          - service_name: "web-svc"
            port: 8080
            weight: 100
    - collection: "apis"
      mapper:
        url_field: "endpoint"
        path: "/api"
        tls: true
        backends:
          - service_name: "api-svc"
            port: 3000
            weight: 100

certificate:
  provider: "vault"
  vault_address: "http://vault:8200"
  vault_role: "route-issuer"
  vault_mount: "pki"
  vault_ttl: "720h"

certificate_store:
  enabled: true
  provider: "vault"
  vault_address: "http://vault:8200"
  vault_mount: "secret"
  vault_prefix: "route-tls"
  renew_before: "720h"

provisioner:
  provider: "netscaler"
  endpoint: "https://10.0.0.1"
  username: "nsroot"
  password: "secret"
  insecure_skip_verify: true

reconcile:
  interval: "5m"

leader_election:
  enabled: true
  provider: "mongo"
  lease_name: "routes-provisioner-leader"
  lease_duration: "15s"
  retry_interval: "2s"
```

### Kubernetes-native with external certificate management

Best when the application does not ship its own ingress controller but you want to stay Kubernetes-native and delegate TLS to an external tool like cert-manager. Routes are managed as Kubernetes Ingress resources, packed into a configurable number of Ingress objects to minimize API server load. The provisioner references TLS Secrets by a default naming convention (`tls-<hostname>`), which cert-manager or a cluster administrator can fulfil independently.

**Pipeline:** MongoDB change stream &rarr; Ingress provisioner &rarr; Kubernetes Ingress (packed rules)

Certificate issuance and storage are handled outside this application (e.g. cert-manager watches the Ingress annotations and creates the matching Secrets). The provisioner only needs to know the Secret naming convention so it can set the `tls.secretName` field on the Ingress.

**Example configuration:**

```yaml
datasource:
  provider: "mongodb"
  uri: "mongodb://mongo:27017"
  database: "mydb"
  collections:
    - collection: "routes"
      mapper:
        url_field: "url"
        path: "/"
        tls: true
        backends:
          - service_name: "app-svc"
            port: 8080
            weight: 100

certificate:
  provider: "selfsigned"            # placeholder — cert-manager handles real issuance

certificate_store:
  enabled: false                    # Secrets are managed by cert-manager

provisioner:
  provider: "ingress"
  namespace: "default"
  max_routes_per_ingress: 50
  ingress_class: "nginx"

reconcile:
  interval: "5m"

leader_election:
  enabled: true
  provider: "kube"
  lease_name: "routes-provisioner-leader"
  namespace: "default"
  lease_duration: "15s"
  renew_deadline: "10s"
  retry_interval: "2s"
```

### Kubernetes-native with integrated certificate management

Same Kubernetes-native Ingress approach, but certificates are issued and stored by this application using certstore-kube. The ingress provisioner automatically references the correct TLS Secrets because the secret naming function is injected from certstore-kube — a single source of truth, no configuration duplication.

**Pipeline:** MongoDB change stream &rarr; Vault PKI issuer &rarr; certstore-kube (K8s Secrets) &rarr; Ingress provisioner

**Example configuration:**

```yaml
datasource:
  provider: "mongodb"
  uri: "mongodb://mongo:27017"
  database: "mydb"
  collections:
    - collection: "routes"
      mapper:
        url_field: "url"
        path: "/"
        tls: true
        backends:
          - service_name: "app-svc"
            port: 8080
            weight: 100

certificate:
  provider: "vault"
  vault_address: "http://vault:8200"
  vault_role: "route-issuer"
  vault_mount: "pki"
  vault_ttl: "720h"

certificate_store:
  enabled: true
  provider: "kube"
  namespace: "default"
  secret_prefix: "route-tls"
  renew_before: "720h"

provisioner:
  provider: "ingress"
  namespace: "default"
  max_routes_per_ingress: 50

reconcile:
  interval: "5m"

leader_election:
  enabled: true
  provider: "kube"
  lease_name: "routes-provisioner-leader"
  namespace: "default"
  lease_duration: "15s"
  renew_deadline: "10s"
  retry_interval: "2s"
```

## Building

Requires Go 1.25.4+. Build individual modules from their directory, or use the workspace from the root.

## Adding a new implementation

1. Create `impl/<type>-<name>/` and init a Go module
2. Add to `go.work` use block
3. Implement the core interface with an extension interface for customization
4. Run `go work sync` followed by `go build ./...`

## License

TBD
