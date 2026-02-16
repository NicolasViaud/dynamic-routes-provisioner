# CLAUDE.md

## Project overview

Dynamic Route Provisioner — a Go workspace monorepo that automates TLS route provisioning on gateway/proxy appliances. Runs inside Kubernetes. Plugin-based: core interfaces in `core/`, implementations in `impl/`, use-case apps in `cmd/`.

## Repository layout

```
go.work                            # Go workspace root
core/                              # Interfaces + domain model (no external deps)
  route.go                         # Domain types: RouteRequest, Certificate, RouteEvent, ...
  trigger/                         # Trigger interface
  certificate/                     # Issuer interface
  provisioner/                     # RouteProvisioner interface (incl. List, Batch*)
  desired/                         # DesiredStateProvider interface
  reconciler/                      # Reconciler — compares desired vs actual, batch-applies diff
  lease/                           # LeaderElector interface
  orchestrator/                    # Orchestrator — wires everything, supports leader election
impl/                              # Implementation modules (each its own Go module)
  source-mongo/                    # MongoDB change stream trigger + desired state (ext: DocumentMapper)
  cert-acme-dns/                   # ACME DNS-01 issuer (ext: DNSProvider)
  cert-acme-http/                  # ACME HTTP-01 issuer (ext: ChallengeSolver)
  cert-selfsigned/                 # Self-signed certificate issuer (testing)
  cert-vault/                      # HashiCorp Vault PKI issuer (role-based)
  certstore-kube/                  # Certificate store — K8s TLS Secrets (caching decorator)
  certstore-vault/                 # Certificate store — Vault KV v2 (caching decorator)
  provisioner-netscaler/           # Netscaler CPX via Nitro API (ext: ResourceMapper)
  lease-mongo/                     # MongoDB-based leader election
  lease-kube/                      # Kubernetes Lease API leader election (coordination/v1)
cmd/
  routes-provisioner/                 # Concrete use-case app (standard Go layout)
    main.go                        # Entrypoint
    internal/config/               # Viper-based config (YAML + ROUTES_* env overrides)
    internal/source/              # MongoMapper (MongoDB routes collection)
    internal/certificate/          # ChallengeServer (HTTP-01 solver)
    internal/provisioner/          # NetscalerMapper (Nitro API resource mapping)
    config.yaml                    # Example config
```

## Build

```bash
# Build a specific module
cd cmd/routes-provisioner && go build ./...
cd impl/source-mongo && go build ./...

# Sync workspace after adding modules
go work sync
```

No tests yet. When adding: `go test ./...` from the module directory.

## Conventions

- Strategy + Pipeline pattern: each impl exposes an extension interface + functional options
- Each impl module: main struct file, extension interface (`mapper.go`/`solver.go`), `options.go`
- Compile-time interface assertions: `var _ <interface> = (*Struct)(nil)`
- `context.Context` on all blocking/IO methods, `log/slog` for logging
- `go.work` for local module resolution (not published remotely yet)
- `cmd/` apps use standard Go layout (`internal/`) with Viper for config
- Env var override convention: `ROUTES_` prefix, `_` as delimiter (e.g. `ROUTES_DATASOURCE_URI`)
- Config uses provider pattern: `datasource.provider`, `certificate.provider`, `certificate_store.provider`, `provisioner.provider`
- Certificate store uses decorator pattern: wraps any `certificate.Issuer`, caches in persistent storage, transparent to reconciler/orchestrator
- `sync.Mutex` in orchestrator serializes event handling and reconciliation

## Adding a new impl module

1. `mkdir impl/<type>-<name>` + `go mod init github.com/nicol/dynamic-route-provisioner/<type>-<name>`
2. Add to `go.work` use block
3. Implement core interface + expose extension interface
4. `go work sync && go build ./...`
