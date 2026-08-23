# Artifact proxy SLO

**Status:** no approved objective. The shipped binary is a composition seam, not a server.

`services/artifact_proxy` provides a reusable grant-checked transfer core, but its binary prints one
line explaining that provider and network composition are deployment-owned, and then exits
(`services/artifact_proxy/src/main.rs:7-12`). Nothing listens, so there is no request stream to
measure, and a liveness or readiness probe has nothing to target. Objectives are defined before
production promotion; this component is not at that point.

## Unratified candidate — not an agreed target

A previous revision recorded `99.9%` availability "for admitted production traffic where
applicable". That line was byte-identical across five unrelated services and carried no owner, no
window, and no measurement. It is preserved here as an **unratified candidate** so the earlier
choice is on the record, and it must not be cited as an agreed commitment. Ratification requires
staging measurements from a real deployment of this service and owner sign-off.

## Indicators that exist today

Unlike the control-plane packages, this service is instrumented. `ProxyMetrics` maintains six
counters — `artifact_proxy.read_requests`, `read_bytes`, `write_requests`, `write_bytes`,
`cache_hits`, `rejected` (`services/artifact_proxy/src/telemetry.rs:16-27`). They live in an
in-process `CounterRegistry` readable only through `snapshot()`; there is **no exporter**, so these
values leave the process only if deployment wiring adds one. That wiring is a prerequisite for any
SLI, not a detail.

## Failure modes that must be reflected in any objective

- **Accounting corruption is a one-way latch.** `mark_accounting_corrupt` clears both
  `accounting_healthy` and `accepting` (`services/artifact_proxy/src/health.rs:98-101`) and fires on
  counter overflow or underflow (`:47`, `:75`). No setter restores either flag, so the instance is
  **restart-only**. Any availability target must budget for whole-instance replacement, not
  in-place recovery.
- **Range reads are not partial reads.** `read_range` calls the full `read` and then slices the
  result (`services/artifact_proxy/src/transfer.rs:81-89`), so a small range of a large object costs
  the whole object in bytes and latency. Latency indicators must not assume range cost scales with
  range size.
- **The cache has no invalidation API.** Digests are verified on insert
  (`services/artifact_proxy/src/cache.rs:63-66`), but the public surface is `new`, `get`, `insert`,
  `bytes`, `entries` (`services/artifact_proxy/src/cache.rs:35,51,63,126,133`) — nothing evicts by
  digest. A garbage-collected object keeps being served from cache until LRU pressure evicts it.
- **Read has no service-level size ceiling.** `write` checks `maximum_write_bytes` before doing work
  (`services/artifact_proxy/src/server.rs:113-118`); `read` (`:72`) has no counterpart, so the only
  ceiling is the grant's `maximum_read_bytes` (`services/artifact_proxy/src/grants.rs:89-94`).

## Bounds already enforced

Grant-scoped read budgets (`grants.rs:89`), service write ceiling
(`server.rs:113`), cache entry and byte capacity (`cache.rs:35`), and digest verification on cache
insert (`cache.rs:64`).

## Correctness invariants (release-blocking, not traded for availability)

No unauthorized or stale-fenced durable commit, ever. Digest verification is never skipped to
restore throughput. Bounded admission, cancellation, and shutdown budgets must be release-qualified
before production promotion; they are not release-qualified today. SLO exclusions require an
incident or evidence record, not an ad hoc dashboard annotation.
