# `libs/go` module reference

This guide is the repository-wide usage reference for the implemented Go
foundation. It describes what each public module owns, when to import it, which
production adapter to use, and what must remain in the consuming domain.

The governing rule is simple:

> `libs/go` owns reusable mechanisms; `control/`, `services/`, `operators/`, and
> workers own business policy, repositories, generated APIs, reconciliation
> decisions, and executable wiring.

Use the narrowest package that owns the mechanism. Do not create local retry,
health, signal, pagination, resource-version, outbox, inbox, lease, or fault
implementations when a foundation package already exists.

## Required process baseline

Every long-running Go process uses these modules directly or through the
standard production builder:

```text
clock
config
faults
identifiers
requestmeta
observability
retry
servicekit
servicekit/production
```

A production process is assembled as follows:

```text
service-owned provider factory
    -> typed config and immutable config snapshot
    -> provider clients and domain modules
    -> servicekit/production.Builder
    -> stable role and capability validation
    -> servicekit stages
    -> liveness, readiness, drain, cancellation, reverse shutdown
```

The command roots in `services/control_plane/cmd/` and runnable examples in
`examples/go/` show this path. Production code must not silently fall back to
memory adapters.

## Layer 0: foundations

### `clock`

**Owns:** a small time abstraction, real clock, deterministic fake clock, and
clock-related failures.

**Use it for:** lease expiry tests, retry scheduling, idempotency windows,
pagination expiry, key validity, work-queue timing, and deterministic lifecycle
tests.

**Do not use it for:** database-authoritative lease time where the provider
adapter deliberately uses server time.

```go
now := clock.Real{}.Now()
```

In tests, inject `clock.Fake` rather than sleeping.

### `faults`

**Owns:** canonical error codes, reasons, operations, safe structured fields,
retry intent, wrapping, classification, and public-safe projections.

**Use it for:** every error that crosses a package boundary or participates in
retry, telemetry, API rendering, or operator decisions.

```go
return faults.New(
    faults.CodeInvalidArgument,
    "dataset name is invalid",
    faults.WithReason("invalid_dataset_name"),
    faults.WithOperation("registry.CreateDataset"),
    faults.WithRetryPolicy(faults.NoRetry()),
)
```

**Rules:**

- never expose `err.Error()` directly to an external client;
- keep sensitive values out of fields and messages;
- attach explicit retry policy only when replay safety has been established;
- transport mapping belongs in `httpx`, `connectx`, or `grpcx`, not in domain
  packages.

### `identifiers`

**Owns:** canonical typed IDs, UUIDv7 generation/parsing, resource kinds, and
SHA-256 content digests.

**Use it for:** durable resource IDs, event/message IDs, artifact digests,
request-bound identities, and cross-language golden vectors.

```go
id, err := identifiers.NewID(identifiers.Kind("run"))
digest := identifiers.DigestBytes(payload)
```

**Do not add:** domain policy or a separate identifier grammar inside each
service. Domain packages may define aliases or constructors that call this
module.

## Layer 1: stable contracts

### `auth`

**Owns:** transport-neutral principals, credentials, claims, authentication,
authorization decisions, permissions, resources, and context propagation.

**Use it for:** API authentication/authorization, internal workload identity,
service-account policy, and audit actor construction.

**Production pattern:** a service factory constructs concrete authenticators and
authorizers; transport interceptors call the stable contracts. Domain services
receive an authenticated principal/decision, not an HTTP token.

**Does not own:** OIDC provider configuration, tenant entitlements, quota policy,
or route policy.

### `audit`

**Owns:** immutable audit event, actor, action, target, change, validation, and
recorder contracts.

**Use it for:** every security-sensitive or externally visible mutation,
approval, promotion, rollback, credential operation, weight access, and
administrative action.

**Production adapter:** `audit/postgres`.

```text
SQL transaction
    -> domain mutation
    -> audit recorder using the same transaction
    -> outbox append using the same transaction
    -> commit
```

Audit payloads must contain bounded metadata and digests, not model inputs,
credentials, biological datasets, or arbitrary blobs.

### `idempotency`

**Owns:** idempotency keys, request fingerprints, acquisition leases, replay
results, completion, conflict behavior, and store contracts.

**Use it for:** client-initiated mutating APIs such as create run, submit job,
promote release, create dataset snapshot, cancel job, and credential rotation.

**Production adapter:** `idempotency/postgres`.

**Not the same as:** event inbox deduplication. Event consumers use
`coordination/inbox`, which builds on these contracts.

### `requestmeta`

**Owns:** request, correlation, causation, and operation metadata plus
transport-neutral context/text-map propagation.

**Use it for:** every API call, event, outbox record, work item, webhook,
controller reconciliation, and internal operation that must preserve lineage.

```go
ctx = requestmeta.WithContext(ctx, metadata)
metadata, ok := requestmeta.FromContext(ctx)
```

A zero envelope is valid for internal work. Do not invent fake user lineage.
Authentication identity remains in `auth`, and OpenTelemetry trace context
remains in the telemetry adapter.

### `messaging`

**Owns:** bounded provider-neutral messages, publication results,
subscriptions/deliveries, settlement, request metadata, reserved attributes,
and conformance contracts.

**Use it for:** at-least-once event delivery after outbox publication and for
bounded internal event subscriptions.

**Adapters:**

- `messaging/pubsub` for the production Pub/Sub seam;
- `messaging/memory` for deterministic local examples/tests;
- `messaging/messagingtest` for provider conformance.

**Does not own:** domain event schema, workflow state, exactly-once claims,
business retry policy, or long-running work. Long-running work uses
`coordination/workqueue`.

### `pagination`

**Owns:** signed opaque keyset cursors, page-size limits, cursor expiry, filter
binding, ordering binding, and token encoding/verification.

**Use it for:** list APIs over mutable or large collections such as runs, jobs,
artifacts, datasets, models, audit records, ingestion snapshots, and
evaluations.

```go
codec, err := pagination.NewCodec(signer, verifier, clk, 15*time.Minute)
binding := pagination.Binding{
    Scope: "tenant/acme",
    Resource: "runs",
    FilterDigest: normalizedFilterDigest,
    Order: []pagination.Order{{Field: "created_at", Direction: pagination.Descending}},
}
```

Repositories own the keyset SQL query and stable tie-breaker. Never decode a
cursor and reuse it against a different tenant, resource, filter, or order.

### `resourceversion`

**Owns:** monotonic resource versions, strong ETags, mutation preconditions,
parsing, comparison, and transport-neutral optimistic concurrency.

**Use it for:** tenant settings, quotas, releases, routes, deployments, jobs,
webhooks, dataset publication, and any mutable control-plane resource.

```text
HTTP      ETag / If-Match / If-None-Match
Protobuf  resource_version / precondition
Postgres  version column + conditional UPDATE
Events    aggregate_version
```

Blob-store generations remain in `storage/blob`; do not coerce provider object
generations into control-plane resource versions.

### `signing`

**Owns:** detached HMAC-SHA-256 and Ed25519 signatures, key IDs, validity
windows, key sets, rotation overlap, encoding, signing, and verification.

**Use it for:** signed pagination cursors, runtime admission/execution tickets,
route snapshots, artifact grants, and webhooks.

**Does not own:** ticket claims, policy epochs, route policy, tenant
authorization, or revocation decisions. Those belong to `control/` and
`protocols/`.

### `storage/blob`

**Owns:** narrow blob-store contract with canonical keys, attributes, range
reads, conditional writes, generations, digests, and opaque listing.

**Adapters:** `storage/blob/gcs` and `storage/blob/memory`.

**Use it for:** service-owned metadata blobs and object-store access where the
Go control plane must read or write bounded objects. The high-throughput
artifact byte plane remains Rust-owned.

### `storage/cache`

**Owns:** bounded ephemeral key/value caching, TTL, versions, conditional
writes, and cache conformance.

**Adapters:** `storage/cache/redis` and `storage/cache/memory`.

**Use it for:** disposable derived state, rate/policy caches, source metadata,
or bounded registry read acceleration. Never make cache presence the sole
durable source of truth.

### `storage/lease`

**Owns:** fenced distributed lease acquisition, renewal, release, expiry,
tokens, versions, and conformance.

**Adapters:** `storage/lease/postgres` and `storage/lease/memory`.

**Use it for:** singleton leadership, claim ownership, maintenance locks, and
other ownership that must reject stale holders.

A stale token must never be allowed to renew, release, or commit after a newer
holder has acquired the lease.

### `storage/sql/transaction`

**Owns:** explicit `database/sql` transaction context, begin/commit/rollback
behavior, and safe transaction boundary errors.

**Use it for:** domain mutation + audit + outbox and projector effect + inbox +
cursor transactions.

**Rule:** transactions are explicit. This package does not perform automatic
transaction retries because replay safety belongs to the caller.

## Layer 2: runtime and coordination mechanisms

### `config`

**Owns:** strict fields, source precedence, unknown-key rejection, validation,
secret/reload metadata, immutable snapshots, deterministic digest, provenance,
redaction, and atomic last-known-good reload.

```go
loader, err := config.New([]config.Field{
    {Key: "database_dsn", Required: true, Secret: true},
    {Key: "listen_address", Default: config.String(":8080")},
}, fileSource, config.EnvSource{Mapping: envMapping})
snapshot, err := loader.Load(ctx)
```

Service-specific typed decoding remains in the service. No production package
may read environment variables behind the config snapshot.

### `observability`

**Owns:** provider-neutral logging, metric records, request/trace propagation
coordination, resource metadata, service lifecycle components, redaction, and
safe error handling.

**Use it for:** every service, worker, controller, dispatcher, projector, and
maintenance process.

Concrete OpenTelemetry SDK/exporter construction belongs in the service
factory. The module does not install hidden global state.

### `retry`

**Owns:** bounded context-aware retry execution, backoff/jitter, attempts,
observers, shared budgets, and explicit retry classification.

```go
result, err := retry.Execute(ctx, policy, func(ctx context.Context, attempt retry.Attempt) error {
    return callProvider(ctx)
})
```

**Rule:** an error is not retryable merely because it looks transient. Either
its `faults.RetryPolicy` or a narrowly scoped classifier must establish replay
safety.

### `servicekit`

**Owns:** deterministic component stages, startup, concurrent run loops,
readiness/liveness, drain, task ownership, signal integration, reverse-order
shutdown, time budgets, observer hooks, and build metadata.

Use `servicekit` directly inside reusable mechanisms and tests. Production
executables use `servicekit/production` as the composition gate.

Every long-lived goroutine must be represented as an owned component or task.
Readiness must fail before drain begins.

### `servicekit/production`

**Owns:** stable process roles, required capability manifests, builder
validation, canonical lifecycle stages, diagnostics, and the mandatory
production assembly path.

```go
builder, err := production.NewBuilder(
    "registry",
    production.RoleRegistry,
    servicekit.WithStartupTimeout(30*time.Second),
    servicekit.WithShutdownTimeout(30*time.Second),
)
err = builder.AddCapability(production.CapabilityObservability, telemetryComponent)
// Add every required capability and domain component.
runtime, err := builder.Build()
```

A process cannot start if required capabilities for its role are absent.
Memory adapters may be used by runnable examples but must not be hidden inside
production factories.

### `coordination/cursor`

**Owns:** monotonic compare-and-swap stream/projection cursors.

**Use it for:** ingestion source watermarks, ordered projectors, replayable event
streams, and maintenance progress.

**Adapters:** memory and PostgreSQL.

Cursor advancement must be in the same transaction as the effects it proves.

### `coordination/inbox`

**Owns:** duplicate-safe event processing built on the idempotency contract,
consumer group and handler version identity, payload-digest integrity, and
transactional completion.

**Use it for:** projectors, usage aggregation, metadata/lineage projection,
registry projections, and other at-least-once consumers.

Receiving the same event ID with a different digest is an integrity failure,
not a duplicate success.

### `coordination/outbox`

**Owns:** durable event records, transactional append contract, fenced claims,
claim expiry, publication attempts, retry/dead-letter state, bounded dispatcher,
and conformance tests.

**Adapters:** memory and PostgreSQL. This is the only outbox namespace. A
`storage/outbox` façade delegating to it existed and was removed: it carried no
behaviour of its own, nothing ever imported it, and a second import path for
one mechanism defeats the rule that a consumer imports the package that owns
it.

```text
request transaction commits outbox record
    -> dispatcher claims records
    -> publishes through messaging
    -> records published/retry/dead-letter transition
```

Do not execute long-running domain work in the dispatcher.

### `coordination/projector`

**Owns:** an owned `servicekit` projector loop that combines event delivery,
inbox processing, domain effect handler, and monotonic cursor advancement.

**Use it for:** materialized views and read models that consume ordered events.
The domain owns the projection effect; the foundation owns lifecycle,
deduplication, and cursor mechanics.

### `coordination/workqueue`

**Owns:** immutable work items, queue claims, fencing tokens, lease heartbeat,
claim-loss cancellation, bounded worker concurrency, completion, retry, and
dead-letter transitions.

**Adapters:** memory and PostgreSQL.

**Use it for:** schedulers, ingestion stages, controllers, operators,
maintenance, webhooks, and other durable long-running work.

```go
worker, err := workqueue.NewWorker(store, workqueue.HandlerFunc(handler), workqueue.WorkerConfig{
    Owner: "ingestion-1",
    Queues: []string{"reference-ingestion"},
    LeaseDuration: 30 * time.Second,
    HeartbeatInterval: 10 * time.Second,
    Concurrency: 8,
})
```

A handler must honor cancellation and must not commit after claim loss.

### `coordination/leadership`

**Owns:** a service-managed leader elector over `storage/lease`, fenced
leadership sessions, acquire/renew/release loops, readiness policy, and bounded
shutdown.

**Use it for:** schedulers, controllers, operators, migration/maintenance
leaders, and singleton dispatch authority.

Domain logic receives a leadership session. It does not own election
goroutines.

## Layer 3: production adapters

### `audit/postgres`

PostgreSQL recorder for the `audit` contract. Use it within the same explicit
transaction as the mutation being audited. Table naming is validated; DDL is
available for service-owned migrations.

### `idempotency/postgres`

PostgreSQL implementation of acquisition, lease renewal/reclamation,
completion, replay, conflict, and stale-token rejection. All services that
provide durable mutating APIs should use it unless a documented throughput
requirement justifies another qualified provider.

### `coordination/*/postgres`

Production adapters for cursor, outbox, and work queue. They preserve the same
contracts and conformance tests as memory implementations. PostgreSQL is the
default authority for durable control-plane coordination.

### `messaging/pubsub`

Canonical Pub/Sub seam. It keeps provider SDK types at the adapter boundary and
standardizes validation, lineage attributes, bounded concurrency, settlement,
retry classification, telemetry, and shutdown.

### `storage/sql/postgres`

Owns PostgreSQL pool configuration, ping/readiness, safe qualified identifiers,
SQLSTATE qualification, and a `servicekit` pool component. It is not an ORM and
does not hide `database/sql`.

### `storage/sql/migrate`

Owns forward-only checksummed migrations, unknown-version and checksum-drift
rejection, advisory locking, planning, receipts, and startup integration.
Migration SQL remains under the service owning the schema.

### `storage/blob/gcs`

GCS implementation of `storage/blob`; stages uploads, enforces size and digest
before visibility, preserves conditional-generation semantics, and keeps GCS
CRC32C enabled as an additional transport check.

### `storage/cache/redis`

Redis implementation of `storage/cache`; uses atomic Lua scripts and server
time for TTL/version decisions. The service factory owns credentials and
provider lifecycle.

### `storage/lease/postgres`

PostgreSQL fenced lease store using server time and conditional token/version
checks. Use it for durable distributed ownership, including
`coordination/leadership`.

### `kubernetes/*`

The Kubernetes namespace provides narrow adapters rather than scheduling
policy:

| Package | Use |
|---|---|
| `kubernetes/client` | client configuration, discovery, and factory |
| `kubernetes/controller` | reconciler lifecycle, recovery, request metadata, and qualification hooks |
| `kubernetes/conditions` | canonical status condition mutation |
| `kubernetes/events` | structured Kubernetes event recording |
| `kubernetes/finalizers` | finalizer add/remove/validation |
| `kubernetes/metadata` | labels and annotations |
| `kubernetes/ownerrefs` | owner-reference construction |
| `kubernetes/patch` | safe patch helpers |
| `kubernetes/status` | status update helpers |
| `kubernetes/watch` | bounded watches and recovery |

Global quota, fair share, topology policy, model resource estimates, and
workflow semantics remain in `control/scheduling` and `control/orchestration`.

## Layer 4: transport adapters

### `httpx`

**Owns:** server/client construction, explicit timeouts, JSON/body limits,
problem responses, headers, propagation, health handlers, middleware stack, and
OpenTelemetry adapter seams.

Use it for every HTTP boundary. Do not use `http.DefaultClient`,
`http.DefaultTransport`, ad hoc panic recovery, or direct error-string
responses.

### `httpx/outbound`

**Owns:** hardened external HTTP policy including HTTPS enforcement, host
allowlists, DNS/IP validation, private/loopback/link-local/metadata rejection,
redirect revalidation, response byte/media/encoding limits, connection limits,
timeouts, user agent, and TLS policy.

Use it for webhooks, HTTP ingestion, partner callbacks, and external evaluation
submissions. Domain webhook signatures and delivery state remain outside.

### `connectx`

**Owns:** Connect server/client configuration, interceptors, authentication,
authorization, validation, request metadata, fault translation, health,
reflection, and OpenTelemetry seams.

Use generated Connect handlers at the service boundary and keep domain services
transport-neutral.

### `grpcx`

**Owns:** gRPC server/client configuration, TLS credentials, interceptors,
request metadata, validation, recovery, fault details, health synchronization,
reflection, and OpenTelemetry seams.

Use it for internal RPC when Protobuf/gRPC is the selected surface.

## Test and local-only modules

Packages such as `*test`, `obstest`, `httpxtest`, `grpctest`, `connecttest`,
`blobtest`, `cachetest`, `leasetest`, `messagingtest`, `outboxtest`, and
`workqueuetest` provide deterministic adapters or conformance suites. Bazel
marks them test-only; production targets must not depend on them.

Memory adapters are suitable for:

- unit and conformance tests;
- local examples;
- deterministic fault injection;
- single-process development where non-durability is explicit.

They are not production fallbacks.

## Canonical recipes

### Mutating API

```text
authenticate and authorize
    -> validate idempotency key/fingerprint
    -> validate resource-version precondition
    -> begin SQL transaction
    -> mutate domain rows
    -> record audit event
    -> append outbox event
    -> commit
    -> return resource and version
```

Packages: `auth`, `idempotency`, `resourceversion`, `storage/sql/transaction`,
`audit/postgres`, `coordination/outbox/postgres`, transport adapter,
`servicekit/production`.

### Event projector

```text
message delivery
    -> verify schema and digest
    -> begin transaction
    -> inbox acquire/deduplicate
    -> apply projection effect
    -> compare-and-advance cursor
    -> complete inbox
    -> commit
    -> acknowledge delivery
```

Packages: `messaging`, `coordination/inbox`, `coordination/cursor`,
`coordination/projector`, `storage/sql/transaction`, `servicekit/production`.

### Durable worker/controller

```text
leadership (where required)
    -> claim fenced work item
    -> heartbeat claim
    -> run cancellation-aware domain handler
    -> complete/retry/dead-letter
    -> drain stops new claims
```

Packages: `coordination/leadership`, `coordination/workqueue`, `storage/lease`,
`retry`, `observability`, `servicekit/production`; add `kubernetes/*` for
reconciliation.

### Event dispatcher

```text
outbox claim
    -> publish through messaging adapter
    -> mark published or retry/dead-letter
    -> readiness and drain through servicekit
```

See `examples/go/event_dispatcher`.

### Ingestion coordinator

```text
leadership
    -> source cursor
    -> leased stage work
    -> artifact/cache/Kubernetes dependencies
    -> append stage event to outbox
    -> separate dispatcher publishes
```

See `examples/go/ingestion_coordinator`.

## Adding a new shared Go package

A package may enter `libs/go` only when:

1. at least two independent consumers need the mechanism;
2. the contract is domain-neutral and has a clear layer;
3. provider and transport dependencies do not leak downward;
4. correctness behavior is defined through tests or a conformance suite;
5. a Bazel target, README, ownership, limits, and failure semantics exist;
6. the addition removes duplicated correctness/operations risk;
7. no existing narrow package can own the capability.

Rejected names include `common`, `shared`, `helpers`, `utils`, `platform`, and a
generic all-purpose repository package.
