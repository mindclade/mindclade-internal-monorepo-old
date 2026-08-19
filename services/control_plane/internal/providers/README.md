# Control-plane provider construction

This package is where the control plane names concrete infrastructure. Every
other service package speaks to a `libs/go` contract; only this one knows that
audit is PostgreSQL, that artifacts are Google Cloud Storage objects, and that
the read cache is Redis.

```text
config.Settings
    -> mechanisms   (clock, identifiers, observability, retry, signing, pagination)
    -> database     (pool, migrations, transactions, audit, idempotency, outbox)
    -> blob store   (Google Cloud Storage)
    -> cache store  (Redis)
    -> identity     (service API keys, permission authorization)
    -> serving      (listener, canonical middleware stack, health)
    -> foundation.Dependencies + bootstrap.Components
```

## Materialized roles

| Role | Factory | Providers |
|---|---|---|
| `registry` | `NewRegistryFactory` | PostgreSQL, Google Cloud Storage, Redis |
| `event-dispatcher` | `NewEventDispatcherFactory` | PostgreSQL, broker |
| every other role | `bootstrap.UnconfiguredFactory` — fails closed with exit 78 | — |

A role is materialized when its factory constructs real providers. Until then
its command starts, validates its profile, and refuses to run. `--describe-profile`
works either way, because deployment tooling needs the manifest before the
adapters exist.

## Rules

- No in-memory adapter is reachable from this package, with one bounded
  exception: the messaging provider, because no Pub/Sub SDK is in `go.mod`.
  It is refused outside development or test by two independent gates. Do not
  extend the pattern to another store.
- Construction is ordered cheapest-first. Configuration and pure mechanisms
  fail before a socket, connection, or cloud client is opened, and anything
  already opened is released when a later step fails.
- No domain policy: no repositories, route tables, generated handlers, or
  business services are assembled here.
- Missing provider configuration is a startup failure, never a silent
  downgrade to a weaker adapter.

## Configuration

`registry` reads `MINDCLADE_DATABASE_DSN`, `MINDCLADE_BLOB_BUCKET`,
`MINDCLADE_CACHE_ADDRESS`, `MINDCLADE_SIGNING_HMAC_KEY`, and
`MINDCLADE_AUTH_API_KEYS`; see `internal/config` for the full schema and the
exact environment mapping. The API-key registry has the form

```text
subject:sha256hex:permission[,permission][;subject:sha256hex:permission...]
```

and carries service identity only. It is not a user-authentication mechanism
and must not grow into one.
