# Control-plane process foundation

The typed composition boundary between reusable `libs/go` mechanisms and
concrete Go processes. Intentionally **not** a shared business-domain library.

## Aggregates

This was one `Dependencies` struct naming a type from every subsystem. Because
`bootstrap` imported it, every command linked every subsystem: all ten unwired
commands linked an identical 31 `libs/go` packages, and two of them compiled to
byte-identical sizes. The substrate is now one package per capability cluster,
and `bootstrap` names none of them.

| Package | Type | Provides |
|---|---|---|
| `foundation` | `Core` | clock, configuration, identifiers, request lineage, telemetry, retry |
| `persistence` | `SQL` | pool, migrations, transaction boundary |
| `governance` | `Controls` | audit, idempotency, signing, pagination, resource versions |
| `identity` | `Controls` | authentication, authorization |
| `objects` | `Stores` | blob store, cache |
| `leasing` | `Mechanisms` | lease store, leader election |
| `eventing` | `Mechanisms` | messaging endpoints, outbox store and dispatcher |
| `tasks` | `Mechanisms` | work queue, workers |
| `projection` | `Mechanisms` | inbox, cursors, projectors |
| `egress` | `Client` | policy-bound outbound HTTP |

A factory returns the aggregates its role needs, and that list *is* the role's
capability profile:

```go
Dependencies: []bootstrap.Aggregate{core, sql, governance, identity, objects, eventing}
```

Anything not in the list is a package the binary does not link. The generated
`bootstrap/consumption.json` is the record of that, and presubmit regenerates
it from the import graph.

## Writing an aggregate

Each one exposes a single `declarations()` list that drives both `Capabilities`
and `Register`, so the two can never disagree about what a process provides. A
`Declaration` with no `Component` is passive; one with a component owns a
lifecycle and is staged by `servicekit/production`.

Capability validation stays in `servicekit/production`: an aggregate reports
what it has, and the builder decides whether the role is satisfied.

Tenancy, admission, routing, ingestion, scheduling, registry, usage, webhook,
and administrative policy stay under `control/` and service adapters.
