# Using the Mindclade Go foundation

This guide answers two questions:

1. Which `libs/go` package owns a mechanism?
2. How should a production Go process compose it without moving domain policy
   into the foundation?

The module path is `go.mindclade.dev`. Imports therefore look like:

```go
import "go.mindclade.dev/libs/go/faults"
```

## Choose the narrowest package

| Need | Package | Notes |
|---|---|---|
| Deterministic or real time | `clock` | Inject clocks into retries, leases, workers, and tests |
| Structured safe failures | `faults` | Stable code/reason/operation/fields/retry intent |
| Canonical UUIDv7 resource IDs and digests | `identifiers` | Kind-prefixed IDs and canonical parsing |
| Authn/authz contracts | `auth` | Principals, claims, resources, permissions, decisions |
| Immutable audit events | `audit` | Use `audit/postgres` for transactional persistence |
| Request/correlation/causation lineage | `requestmeta` | Propagate across transports and event envelopes |
| Replay-safe API mutations | `idempotency` | Use `idempotency/postgres` in production |
| Strict resolved configuration | `config` | Provenance, digest, redaction, safe reload |
| Explicit retry policy and budgets | `retry` | Never infer replay safety from an error alone |
| Logs, metrics, traces, propagation | `observability` | Bounded attributes and explicit provider lifecycle |
| Process lifecycle | `servicekit` | Components, probes, stages, drain, reverse shutdown |
| Production process assembly | `servicekit/production` | Mandatory role/capability validation |
| Broker-neutral delivery | `messaging` | At-least-once contract; pair with inbox/outbox |
| Opaque keyset cursors | `pagination` | Bind tokens to scope/filter/sort and sign them |
| Optimistic concurrency | `resourceversion` | Map to ETag/If-Match and conditional SQL updates |
| Detached signatures | `signing` | Mechanism only; claim semantics remain in domains |
| Durable event publication | `coordination/outbox` | Insert in the same SQL transaction as domain state |
| Idempotent event application | `coordination/inbox` | Deduplication around transactional handlers |
| Monotonic projector/source position | `coordination/cursor` | Compare-and-advance; never blind overwrite |
| Event projection loop | `coordination/projector` | Source + inbox + cursor + handler |
| Leased background work | `coordination/workqueue` | Fencing, heartbeat, retry, dead-letter state |
| Singleton authority | `coordination/leadership` | Lease-backed leadership with fencing |
| Blob/object access | `storage/blob` | GCS and memory adapters; immutable references |
| Caching | `storage/cache` | Redis and memory adapters; not a source of truth |
| Fenced leases | `storage/lease` | PostgreSQL and memory adapters |
| SQL transaction context | `storage/sql/transaction` | Share a transaction across domain/audit/outbox |
| PostgreSQL lifecycle | `storage/sql/postgres` | Pool settings, health, and failure classification |
| Forward-only migrations | `storage/sql/migrate` | Checksums, locking, dirty-state detection |
| Inbound HTTP | `httpx` | Bounded bodies, structured faults, health, middleware |
| Hardened outbound HTTP | `httpx/outbound` | SSRF/redirect/body/TLS policy |
| Connect RPC | `connectx` | Standard interceptors, health, reflection, OTel |
| gRPC | `grpcx` | Standard TLS, interceptors, health, reflection, OTel |
| Kubernetes reconciliation | `kubernetes/*` | Clients, controllers, status, patches, finalizers, watches |

Memory and `*test` packages are local/conformance adapters, not production
persistence.

## Create structured faults

```go
return faults.New(
    faults.CodeInvalidArgument,
    "run configuration is invalid",
    faults.WithReason("invalid_run_config"),
    faults.WithOperation("runs.Service.Create"),
    faults.WithField("field", "model"),
    faults.WithRetryPolicy(faults.NoRetry()),
)
```

Wrap diagnostic causes while keeping a client-safe public message:

```go
return faults.Wrap(
    err,
    faults.CodeUnavailable,
    "registry storage is unavailable",
    faults.WithReason("registry_store_unavailable"),
    faults.WithOperation("registry.Repository.Get"),
    faults.WithRetryPolicy(faults.ImmediateRetry(250*time.Millisecond)),
)
```

Never serialize `err.Error()` to a client. Use the standard HTTP, Connect, or
gRPC fault renderer.

## Create canonical identifiers

```go
runKind := identifiers.MustParseKind("run")
runID, err := identifiers.NewID(runKind)
if err != nil {
    return err
}
```

Resource IDs are `<kind>_<32 lowercase UUIDv7 hex characters>`. Parse and
validate at boundaries; store the canonical representation. Use content digests
for immutable bytes, not resource IDs.

## Assemble every production service through servicekit

```go
builder, err := production.NewBuilder("registry", production.RoleRegistry)
if err != nil {
    return err
}

// Passive mechanisms are declared after their concrete dependencies exist.
for _, capability := range []production.Capability{
    production.CapabilityClock,
    production.CapabilityConfiguration,
    production.CapabilityIdentifiers,
    production.CapabilityRequestMetadata,
    production.CapabilityRetry,
} {
    if err := builder.Declare(capability); err != nil {
        return err
    }
}

if err := builder.AddCapability(
    production.CapabilityObservability,
    telemetryComponent,
); err != nil {
    return err
}
if err := builder.AddCapability(
    production.CapabilityDatabase,
    databaseComponent,
); err != nil {
    return err
}
if err := builder.AddWork(registryEngineComponent); err != nil {
    return err
}
if err := builder.AddCapability(production.CapabilityHTTP, httpComponent); err != nil {
    return err
}

runtime, err := builder.Build()
if err != nil {
    return err // missing role requirements fail closed
}
return runtime.Run(ctx)
```

Use the process-specific factory and shared bootstrap under
`services/control_plane/internal/`; command roots do not construct providers or
own signals directly.

## Transactional mutation, audit, and outbox

The canonical durable write is:

```text
idempotency acquire / resource-version check
    -> begin SQL transaction
    -> domain repository mutation
    -> append audit record using the same transaction
    -> append outbox event using the same transaction
    -> commit
```

A separate `coordination/outbox.Dispatcher` claims committed records with a
fencing token and publishes through `messaging`. It acknowledges only with the
current claim token/version. Do not publish directly from the request handler
and do not create service-specific outbox loops.

See the runnable example:

```bash
go run ./examples/go/event_dispatcher
```

## Idempotent event projection

Use this composition:

```text
messaging delivery
    -> coordination/inbox transaction
    -> domain projector effects
    -> coordination/cursor compare-and-advance
    -> optional outbox append
    -> commit
    -> acknowledge delivery
```

The event ID, consumer group, handler version, and payload digest protect
against duplicate or conflicting deliveries. A duplicate is not an error; the
same event ID with a different digest is an integrity failure.

## Leased background work

Schedulers, controllers, operators, ingestion coordinators, and maintenance
processes use `coordination/workqueue`:

```text
enqueue immutable work item
    -> claim with owner, lease deadline, and fencing token
    -> execute under bounded concurrency
    -> heartbeat before lease expiry
    -> cancel on claim loss
    -> complete, retry with bounded delay, or dead-letter
```

The queue owns claim mechanics; the domain handler owns what the work means.
Always include the fencing token in state/output commits that could race a
replacement worker.

## Leadership

Use `coordination/leadership` over `storage/lease`. The handler receives a
`leadership.Session`; its `Fence()` value must protect writes where a stale
leader could otherwise commit after replacement. Register the elector's
component with `servicekit/production`, not as a detached goroutine.

## Strict configuration

Define a finite field schema, layer explicit sources, reject unknown keys,
validate once, and pass an immutable snapshot into constructors. Record the
resolved digest in process diagnostics. Secrets must be redacted and preferably
represented as references. Reload only fields explicitly marked reloadable;
retain the last-known-good snapshot on failure.

## Pagination and resource versions

List APIs use signed keyset cursors, never unbounded offset scans. Bind the
cursor to tenant/workspace, resource kind, normalized filter digest, sort order,
last key, schema version, expiration, and signing key ID.

Mutable resources carry a monotonically increasing `resourceversion.Version`.
Map it consistently:

```text
HTTP       ETag / If-Match / If-None-Match
Protobuf   resource_version + precondition
Events     aggregate_version
Postgres   version column + conditional update
```

## Transport selection

- REST/JSON and health endpoints: `httpx`.
- Browser-friendly generated RPC: `connectx`.
- internal streaming or established gRPC integrations: `grpcx`.
- external callbacks and source retrieval: `httpx/outbound`.

Use standard authentication, authorization, request metadata, recovery,
validation, fault mapping, body limits, OTel, readiness, and shutdown stacks.
Do not construct default clients/servers directly in domain packages.

## Provider composition

Provider adapters are created only in service composition roots:

```text
PostgreSQL       storage/sql/postgres plus package-specific postgres adapters
Redis            storage/cache/redis
GCS              storage/blob/gcs
Pub/Sub          messaging/pubsub
Kubernetes       kubernetes/client and focused subpackages
```

A domain package depends on the narrow contract. Provider conformance suites
must run against the real pinned dependency in connected CI.

## Testing

Use package-specific conformance kits rather than hand-written provider tests:

```text
connecttest, grpctest, httpxtest, obstest
idempotencytest, messagingtest
cursortest, outboxtest, workqueuetest
blobtest, cachetest, leasetest, sqltest
```

Useful commands:

```bash
go test ./libs/go/...
go test -race ./libs/go/...
tools/qualification/go/validate.sh offline

go run ./services/control_plane/cmd/api --describe-profile
go run ./services/control_plane/cmd/scheduler --describe-profile
```

The `--describe-profile` output is the machine-readable contract for the
required foundation capabilities of each process role.

## Promotion rule for new shared packages

A package belongs in `libs/go` only when two or more independent consumers need
the same domain-neutral mechanism, moving it reduces correctness or operational
risk, its layer is unambiguous, and it has tests, docs, Bazel ownership, and
visibility constraints. Domain nouns such as runs, quotas, datasets, models,
routes, ingestion stages, or scheduling policy belong under `control/`.
