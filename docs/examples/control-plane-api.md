# Integration example: control-plane API process

The API command is intentionally tiny:

```go
func main() {
    os.Exit(bootstrap.Main(
        context.Background(),
        bootstrap.RoleAPI,
        deploymentProviderFactory,
        os.Args[1:],
        os.Stdout,
        os.Stderr,
    ))
}
```

The deployment provider factory constructs concrete PostgreSQL, identity,
audit, idempotency, outbox, signing, pagination, observability, and HTTP/Connect
or gRPC dependencies. It returns `bootstrap.Runtime`; the shared bootstrap then:

1. validates the API role and components;
2. maps concrete dependencies to the canonical production capabilities;
3. delegates lifecycle ordering to `servicekit/production.Builder`;
4. starts telemetry, infrastructure, coordination, work, then serving;
5. withdraws readiness before drain;
6. drains and stops in reverse order with bounded telemetry flush.

The scaffold command uses a `nil` factory on purpose. `--describe` emits the
required capability/package manifest, while normal startup fails closed. This
prevents a scaffold binary from silently using in-memory stores or fabricated
credentials in production.

Relevant source:

```text
services/control_plane/cmd/api/main.go
services/control_plane/internal/bootstrap/
services/control_plane/internal/foundation/
libs/go/servicekit/production/
control/admission/foundation.go
control/runs/foundation.go
control/tenancy/foundation.go
```
