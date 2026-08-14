# Production Go service assembly

`servicekit/production` is the mandatory composition guardrail for Mindclade Go
processes. It keeps four concerns synchronized:

1. mechanisms actually constructed by a process;
2. requirements of its stable process role;
3. canonical `servicekit` startup and reverse shutdown stages; and
4. the immutable runtime plan exposed to diagnostics and qualification.

It contains no provider constructors and no domain policy. PostgreSQL, Redis,
object storage, Kubernetes, transports, coordination stores, generated API
handlers, and domain engines are constructed by their owning service and
registered through `Builder`.

## Required path

```text
construct and validate dependencies
    -> production.NewBuilder(service, role)
    -> Declare(passive mechanism)
    -> AddCapability(lifecycle-owning mechanism)
    -> AddWork(domain engine)
    -> Build()
    -> Runtime.RunWithSignals(ctx)
```

Production commands must not call `servicekit.New`, install process-local signal
handlers, or launch detached coordination loops directly.

## Capability precision

Durable stores and the loops that consume them are separate capabilities:

```text
outbox_store       != outbox_dispatcher
work_queue_store   != work_queue_worker
cursor_store       != projector
lease_store        != leadership
```

This prevents an API that merely inserts outbox records from accidentally
satisfying the dispatcher profile, and prevents a scheduler with a queue store
but no leased worker from appearing production-ready.

## Canonical stages

```text
foundation      observability
infrastructure  database lifecycle, Kubernetes manager
coordination    leadership, outbox dispatcher
work            projector, work-queue worker, domain engines
serving         HTTP, Connect, gRPC
```

Blob, cache, lease, cursor, inbox, outbox-store, and work-queue-store contracts
are normally passive because their lifecycle is owned by a provider component
or database pool. Authentication, authorization, audit, idempotency, retry,
transactions, identifiers, clock, and request metadata are also passive.

Role profiles cover APIs, schedulers, controllers, operators, ingestion
coordinators, projectors, event and webhook dispatchers, registries,
administrative processes, and maintenance workers. `Build` additionally rejects
API-like roles without a serving component, work roles without a work component,
and dispatchers without a coordination component.
