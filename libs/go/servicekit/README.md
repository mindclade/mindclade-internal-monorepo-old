# Servicekit

`servicekit` is Mindclade's transport-neutral lifecycle foundation for long-running Go services, workers, controllers, operators, and command-line daemons.

It provides one deterministic coordinator for:

- ordered component startup;
- concurrent component run loops;
- reverse-order graceful shutdown;
- total startup and shutdown budgets;
- per-component stop budgets;
- liveness and readiness probe registries;
- operating-system signal handling;
- lifecycle events for observability adapters;
- panic containment at extension boundaries;
- runtime build provenance; and
- immutable service-state snapshots.

It does **not** call `os.Exit`, parse flags or configuration, construct dependency graphs, start a particular HTTP/gRPC framework, or own service business logic.

## Dependency boundary

The production package has two foundational Mindclade dependencies:

```text
libs/go/clock       libs/go/faults
        ▲                 ▲
        └────────┬────────┘
                 │
        libs/go/servicekit
        ▲
        ├── services/...
        ├── libs/go/httpx, connectx, grpcx
        ├── libs/go/kubernetes
        ├── libs/go/storage consumers
        └── service binaries
```

`servicekit` uses `clock` for lifecycle timestamps and package-owned deadlines, and `faults` for stable codes, reasons, operation names, retry intent, safe messages, request/trace metadata, and structured fields. It still preserves standard `errors.Is` and `errors.As` behavior.

Other shared packages integrate without reversing the dependency graph:

- `libs/go/observability` adapts `servicekit.Observer`, `Event.Fields`, `Snapshot.Fields`, and `ProbeResult.Fields` into logs, traces, and metrics.
- `libs/go/httpx`, `libs/go/connectx`, and `libs/go/grpcx` render `Service.Liveness` and `Service.Readiness` through their transport-specific health surfaces.
- `libs/go/kubernetes` clients and controllers are registered as components; `servicekit` does not import Kubernetes APIs.
- `libs/go/auth` and `libs/go/storage` remain application dependencies registered by the service composition root.
- `libs/go/identifiers` is not imported merely to represent local component names. Domain identifiers may be carried in `faults.Fields` by components.
- `libs/go/testkit` may be used by external tests, but production `servicekit` code never imports it.

This keeps the foundational graph acyclic and avoids turning lifecycle coordination into a service framework.

## Basic usage

```go
package main

import (
    "context"
    "errors"
    "net/http"
    "time"

    "go.mindclade.dev/libs/go/faults"
    "go.mindclade.dev/libs/go/servicekit"
)

func run(ctx context.Context) error {
    server := &http.Server{
        Addr:              ":8080",
        ReadHeaderTimeout: 5 * time.Second,
    }

    service, err := servicekit.New(
        "control-plane",
        servicekit.WithStartupTimeout(60*time.Second),
        servicekit.WithShutdownTimeout(30*time.Second),
        servicekit.WithComponentStopTimeout(10*time.Second),
        servicekit.WithProbeTimeout(2*time.Second),
    )
    if err != nil {
        return err
    }

    if err := service.Add(servicekit.Component{
        Name: "http-server",
        Run: func(context.Context) error {
            err := server.ListenAndServe()
            if errors.Is(err, http.ErrServerClosed) {
                return nil
            }
            return faults.Wrap(
                err,
                faults.CodeUnavailable,
                "HTTP server failed",
                faults.WithReason("http_server_failed"),
            )
        },
        Stop: func(ctx context.Context) error {
            return server.Shutdown(ctx)
        },
        Liveness: func(context.Context) error {
            return nil
        },
        Readiness: func(context.Context) error {
            return nil
        },
    }); err != nil {
        return err
    }

    return service.RunWithSignals(ctx)
}
```

The process entry point chooses the exit policy:

```go
func main() {
    if err := run(context.Background()); err != nil {
        // Log through the Mindclade observability boundary and flush telemetry.
        // Process termination belongs here—not inside servicekit.
        os.Exit(1)
    }
}
```

A production `main` should use the repository's logging, telemetry-flush, and exit-code conventions. The important boundary is that `servicekit` returns control to its caller.

## Clock and deterministic deadlines

All package-owned lifecycle budgets and timestamps use the injected Layer 0
clock:

```go
fakeClock := clock.NewFake(time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))

service, err := servicekit.New(
    "qualification-worker",
    servicekit.WithClock(fakeClock),
    servicekit.WithStartupTimeout(30*time.Second),
)
```

This includes startup, total shutdown, per-component stop, and probe deadlines.
Parent context cancellation remains authoritative. The default is
`clock.RealClock{}`.

## Error semantics

Exported operational failures are structured:

```go
err := service.Run(ctx)

code := faults.CodeOf(err)
reason := faults.ReasonOf(err)
operation := faults.OperationOf(err)
fields := faults.FieldsOf(err)
retry := faults.RetryPolicyOf(err)
```

Servicekit also exposes stable sentinel errors for local identity checks:

```go
switch {
case errors.Is(err, servicekit.ErrStartupTimeout):
    // Startup budget was exhausted.
case errors.Is(err, servicekit.ErrShutdownTimeout):
    // Shutdown or run-loop drain budget was exhausted.
case errors.Is(err, servicekit.ErrAlreadyRun):
    // Service instances are intentionally single-use.
}
```

Typical classifications include:

| Failure | `faults.Code` | Reason |
|---|---|---|
| Invalid service/component/probe name | `invalid_argument` | `invalid_*_name` |
| Duplicate component/probe | `already_exists` | `duplicate_component` / `duplicate_probe` |
| Mutation after startup | `failed_precondition` | `service_configuration_frozen` |
| Startup timeout | `deadline_exceeded` | `startup_timeout` |
| Shutdown timeout | `deadline_exceeded` | `shutdown_timeout` |
| Recovered panic | `internal` | `panic_recovered` |
| Unstructured component failure | phase-dependent | `component_*_failed` |

When a component returns an existing Mindclade fault, servicekit preserves its code, reason, fields, and retry policy while adding:

```text
service_name
component_name
lifecycle_phase
```

## Component contract

```go
type Component struct {
    Name      string
    Start     servicekit.Hook
    Run       servicekit.Hook
    Drain     servicekit.Hook
    Stop      servicekit.Hook
    Liveness  servicekit.Probe
    Readiness servicekit.Probe
}
```

Lifecycle rules:

1. `Start` functions run sequentially in registration order.
2. A component is considered started only after its `Start` function succeeds.
3. If startup fails, only successfully started components are stopped.
4. `Run` functions begin after all components have started.
5. The first `Run` function to return initiates shutdown.
6. A nil `Run` result is a graceful component completion.
7. A non-nil `Run` result fails the service unless it is the expected result of service cancellation.
8. `Drain` functions run sequentially in reverse registration order, before any `Run` context is canceled, so listeners and claim loops can stop admitting new work while established work finishes.
9. `Stop` functions run sequentially in reverse registration order.
10. Every hook and probe must honor context cancellation.
11. A hook that ignores cancellation may continue in a leaked goroutine after its budget expires; servicekit cannot forcibly terminate Go code.
12. A `Shutdown` that arrives before `Run` is latched rather than discarded, so the `Run` that follows stops instead of running unsupervised.

## Liveness and readiness

The service contributes a lifecycle probe automatically:

- liveness passes in `starting`, `running`, `draining`, and `stopping` states;
- readiness passes only in the `running` state.

Draining is live and not ready on purpose. Failing liveness during a drain gets
the process killed mid-request, which is the opposite of what the drain is for;
reporting ready during a drain keeps the orchestrator routing new traffic into a
process that is on its way out. `libs/rust/servicekit` answers both probes from
the same table (`LifecycleState::is_live`, `LifecycleState::admits_traffic`), so
a Go process and a Rust node are routed the same way.

## Phase graph

The seven phases and the transitions between them are a cross-language
contract, not a Go implementation detail. `State.CanTransitionTo` is the table,
`libs/rust/servicekit/src/lifecycle.rs` enforces the identical one on every Rust
transition, and both suites pin it edge by edge:

```text
new -> starting -> running -> draining -> stopping -> stopped
        │            │           │            │
        │            └───────────┴────────────┴──> failed
        ├──> draining   (termination requested before running is announced)
        └──> stopping   (startup failure)
```

The two extra edges out of `starting` exist so a process that never served can
still end: neither passes through `running`, which is what keeps readiness false
for a process that never admitted traffic.

Component probes are registered automatically under names such as:

```text
component/database
component/http-server
```

Additional service-level probes can be managed dynamically:

```go
err := service.RegisterReadiness("dependency/model-registry", registryProbe)
removed := service.UnregisterReadiness("dependency/model-registry")
```

Probe execution uses a snapshot of the registry and evaluates probes concurrently. Reports are sorted by probe name for deterministic output.

## Observability integration

Core lifecycle code depends on the small `Observer` contract rather than a concrete logger, tracer, or metric backend:

```go
observer := servicekit.CombineObservers(
    loggingObserver,
    tracingObserver,
    metricsObserver,
)

service, err := servicekit.New(
    "control-plane",
    servicekit.WithObserver(observer),
)
```

Observers must be fast and non-blocking. Panics are isolated. Slow or buffered export belongs in the observability adapter.

Useful adapter inputs include:

```go
event.ErrorCode()
event.Fields()
service.Snapshot().Fields()
service.Readiness(ctx)
service.Liveness(ctx)
```

## Build provenance

```go
build := servicekit.CurrentBuildInfo("control-plane")
attributes := build.Attributes()
```

The result includes Go version, main module, VCS, revision, dirty-tree status, and build time when embedded by the Go toolchain.

## Bazel

```bash
tools/dev/bazelw test //libs/go/servicekit:servicekit_test --config=ci
```

The package intentionally has no nested `go.mod`. It uses the monorepo's authoritative Go module or workspace.

## Qualification

Recommended local qualification:

```bash
go test ./libs/go/servicekit/...
go test -race ./libs/go/servicekit/...
go vet ./libs/go/servicekit/...
tools/dev/bazelw test //libs/go/servicekit:servicekit_test --config=ci
```

The test suite covers ordered startup, reverse shutdown, startup rollback, structured failure propagation, context metadata, probe concurrency, panic containment, signal context behavior, and timeout handling.
