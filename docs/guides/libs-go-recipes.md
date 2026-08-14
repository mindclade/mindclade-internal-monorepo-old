# `libs/go` production recipes

## Recipe 1: durable API mutation

```text
HTTP/Connect/gRPC handler
  -> auth authenticator + authorizer
  -> requestmeta extraction
  -> idempotency acquire using request fingerprint
  -> transaction.Run
       -> resourceversion precondition
       -> domain repository mutation
       -> audit/postgres append
       -> coordination/outbox append
  -> idempotency complete with stable result
  -> transport-safe fault rendering
```

Never publish to the broker before the transaction commits.

## Recipe 2: outbox event dispatcher

Use `coordination/outbox.Dispatcher` with the service-owned store and
`messaging.Publisher`. Register `dispatcher.Component(...)` as
`CapabilityOutboxDispatcher` through `servicekit/production`. See
`examples/go/event_dispatcher`.

## Recipe 3: ordered event projector

```text
messaging.Subscription.Receive
  -> coordination/projector Processor
       -> coordination/inbox Processor
       -> domain handler transaction
       -> coordination/cursor Advance
  -> delivery Ack only after commit
```

The inbox identity is consumer group + event ID + handler version. The same ID
with a different payload digest is an integrity error.

## Recipe 4: long-running durable work

Producers enqueue immutable work records in the transaction that made the work
visible. A `coordination/workqueue.Worker` claims with a fencing token, renews
the lease, cancels work if the claim is lost, and completes/retries/dead-letters
through the current token/version. Domain handlers own the actual work.

## Recipe 5: singleton controller/operator

Use `coordination/leadership.Elector` over `storage/lease`. The leader handler
receives a fenced session and is cancelled when renewal fails. Register the
elector as a servicekit capability; never run a process-local election goroutine.

## Recipe 6: list API

Normalize filters and ordering, compute a filter digest, decode a
`pagination` cursor bound to tenant/workspace/resource/order, execute a keyset
query, and issue the next signed cursor. Apply `resourceversion`/ETag
preconditions to mutations, not list offsets.

## Recipe 7: safe outbound webhook or ingestion source

Construct `httpx/outbound.Client` with an exact hostname allowlist, HTTPS-only
policy, allowed ports/media types, redirect limit, DNS/IP restrictions, size
limit, and explicit timeouts. Webhook signing and delivery state remain in the
webhook domain.

## Recipe 8: process configuration

Define a typed field catalog in `config`, load defaults/file/env/explicit
sources in deterministic order, reject unknown keys, validate, retain source
provenance, redact secrets, and expose the snapshot digest. Reload only fields
marked reloadable; otherwise retain last-known-good and report restart required.

## Recipe 9: standard process lifecycle

Construct providers in dependency order, aggregate them in typed dependencies,
register with `servicekit/production`, attach serving/domain components, build,
and run. Readiness fails before drain; active loops stop claiming new work;
components stop in reverse order within fixed budgets.

## Recipe 10: tests

Use package-specific conformance suites and memory adapters. Test stale fences,
duplicate delivery, lease expiry, retry exhaustion, shutdown, cancellation,
transaction rollback, configuration errors, safe fault projection, and bounded
queues. Provider adapters repeat the same conformance in connected CI.
