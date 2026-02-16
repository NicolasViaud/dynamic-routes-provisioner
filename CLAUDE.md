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
  provisioner/                     # RouteProvisioner interface
  orchestrator/                    # Orchestrator — wires Trigger → Issuer → Provisioner
impl/                              # Implementation modules (each its own Go module)
  trigger-mongo/                   # MongoDB change stream trigger (ext: DocumentMapper)
  cert-acme-http/                  # ACME HTTP-01 issuer (ext: ChallengeSolver)
  provisioner-netscaler/           # Netscaler CPX via Nitro API (ext: ResourceMapper)
cmd/
  sds-provisioner/                 # Concrete use-case app (standard Go layout)
    cmd/sds-provisioner/main.go    # Entrypoint
    internal/config/               # Viper-based config (YAML + SDS_* env overrides)
    internal/trigger/              # WorkspaceMapper (MongoDB workspace collection)
    internal/certificate/          # ChallengeServer (HTTP-01 solver)
    internal/provisioner/          # NetscalerMapper (Nitro API resource mapping)
    config.yaml                    # Example config
```

## Build

```bash
# Build a specific module
cd cmd/sds-provisioner && go build ./...
cd impl/trigger-mongo && go build ./...

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
- `cmd/` apps use standard Go layout (`cmd/`, `internal/`) with Viper for config
- Env var override convention: `SDS_` prefix, `_` as delimiter (e.g. `SDS_MONGODB_URI`)

## Adding a new impl module

1. `mkdir impl/<type>-<name>` + `go mod init github.com/nicol/dynamic-route-provisioner/<type>-<name>`
2. Add to `go.work` use block
3. Implement core interface + expose extension interface
4. `go work sync && go build ./...`
