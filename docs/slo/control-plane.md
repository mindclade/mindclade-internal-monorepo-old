# Control plane process SLO

**Status:** no approved objective. This document records what is measurable and what is bounded
today; it does not set a target.

`services/control_plane` is not one service. It builds **eleven role binaries** from one tree, and
`services/control_plane/PRODUCTION_READINESS.md` evaluates readiness per role, stating that they
are "independent promotion units, not hidden registry dependencies." Ten of the eleven are held
fail-closed.

This document therefore covers only what is genuinely shared across roles: the leadership and
lease contract and the fail-closed composition seams. The bounds table below is per-role, not
inherited: the egress values are `webhook_dispatcher`'s, the `dispatcher*` values
`event_dispatcher`'s, and the `expiration*`/`housekeeping*`/`maintenance*` values `maintenance`'s.
Domain
objectives belong to the `control.*` component documents, which are listed per role below.

## Role inventory

| Role | Domain SLO document | Remaining gate (per `PRODUCTION_READINESS.md`) |
| --- | --- | --- |
| `registry` | `docs/slo/control-model-registry.md` | wired to production PostgreSQL and qualified against live PostgreSQL, but promotion still requires the release run's SBOM, provenance, signature and rollback artifacts, and release promotion is itself held fail-closed in the production factory — no active release-policy digest/epoch or typed evidence resolver is injected (`PRODUCTION_READINESS.md:56-68`) |
| `api` | `docs/slo/control-admission.md` | protected-CI, multi-process/multi-replica, failure-injection and restore evidence; enforcing Gateway proxy; operational SLO approval |
| `admin` | `docs/slo/control-admission.md` | shares the `api` row's gates, and policy administration — `admin`'s own function — is among the items listed as remaining |
| `maintenance` | this document | protected connected execution, multi-replica lease failover, long-running backlog/retention behaviour, operational SLO approval |
| `scheduler` | `docs/slo/control-scheduling.md` | placement handler is not configured |
| `controller`, `operator` | `docs/slo/control-orchestration.md` | domain reconcilers are not registered |
| `event_projector` | `docs/slo/control-orchestration.md` | projection source and handler are not configured |
| `event_dispatcher` | `docs/slo/control-orchestration.md` | no production Pub/Sub adapter |
| `webhook_dispatcher` | `docs/slo/control-orchestration.md` | delivery handler is not configured |
| `ingestion_controller` | `docs/slo/control-ingestion.md` | staging handler is not configured |

An objective cannot be set for a role whose domain handler is absent: the target would be measured
against work the process refuses to perform by design.

## Shared leadership and lease contract

Five singleton roles — `scheduler`, `ingestion_controller`, `controller`/`operator`,
`event_projector`, and `maintenance` — run identical leadership parameters; the maintenance
block even says so in source: "Leadership timings match the other singleton roles." This uniformity is the one cross-role
availability property that can be stated without a per-domain decision.

| Parameter | Value | Source |
| --- | --- | --- |
| `leaseTTL` | 15 s | `scheduler.go:49`, `ingestion.go:51`, `controller.go:50`, `projector.go:48`, `maintenance.go:44` |
| `leaseRenewInterval` | 5 s | `scheduler.go:50`, `ingestion.go:52`, `controller.go:51`, `projector.go:49`, `maintenance.go:45` |
| `leaseAcquireInterval` | 2 s | `scheduler.go:51`, `ingestion.go:53`, `controller.go:52`, `projector.go:50`, `maintenance.go:46` |
| `leaseReleaseTimeout` | 5 s | `scheduler.go:52`, `ingestion.go:54`, `controller.go:53`, `projector.go:51`, `maintenance.go:47` |
| `leaderReadinessRequired` | `true` | `scheduler.go:53` and the four peers above |

Lease keys are distinct per role (`control-plane/scheduler`, `control-plane/ingestion-coordinator`,
`control-plane/controller`, `control-plane/operator`, `control-plane/event-projector`,
`control-plane/maintenance`), so a role
losing leadership cannot displace another.

`internal/bootstrap/promotion_test.go` requires every command to enter through `bootstrap.Main`,
and `internal/bootstrap/profile_test.go` rejects provider capabilities a role's declared profile
does not justify. Standby components have no independent `Run` path.

## Fail-closed composition seams

Eighteen distinct `*_not_configured` fault codes keep unwired seams from reporting successful
work — among them `placement_handler_not_configured`, `projection_source_not_configured`,
`delivery_handler_not_configured`, `staging_handler_not_configured`, and
`pubsub_provider_not_configured`. `bootstrap.UnconfiguredFactory`
(`internal/bootstrap/bootstrap.go:76-87`) fails rather than starting, and does so with
`faults.NoRetry()` — the property that makes the seam fail closed rather than spin.

**These seams are why most roles have no objective, and they must not be counted as availability
failures if one is later set.** A role returning `placement_handler_not_configured` is behaving
correctly.

## Bounds already enforced

Real and assertable today. They constrain any future objective; they are not themselves
objectives.

| Bound | Value | Source |
| --- | --- | --- |
| `egressTimeout` | 20 s | `internal/providers/webhook/webhook.go:59` |
| `egressDialTimeout` | 5 s | `webhook.go:60` |
| `egressResponseTimeout` | 10 s | `webhook.go:61` |
| `egressMaxResponseBytes` | 1 MiB | `webhook.go:62` |
| `egressMaxRedirects` | 3 | `webhook.go:63` |
| `egressMaxConnsPerHost` | 8 | `webhook.go:64` |
| `deliveryBatchSize` / `deliveryConcurrency` | 16 / 8 | `webhook.go:50-51` |
| `deliveryFailureDelay` | 30 s | `webhook.go:52` |
| `brokerCapacity` | 1024 | `internal/providers/broker/broker.go:26` |
| `brokerMaxAttempts` | 5 | `broker.go:27` |
| `brokerAckDeadline` | 30 s | `broker.go:28` |
| `dispatcherPollInterval` | 250 ms | `internal/providers/dispatcher/dispatcher.go:28` |
| `dispatcherClaimDuration` | 30 s | `dispatcher.go:29` |
| `dispatcherBatchSize` | 64 | `dispatcher.go:30` |
| `dispatcherMaxDeliveries` | 8 | `dispatcher.go:31` |
| `expirationBatchSize` / `expirationMaximumBatchSize` | 256 / 1000 | `internal/providers/maintenance/housekeeping.go:33,35` |
| `expirationTerminalRetention` | 7 d | `housekeeping.go:38` |
| `expirationPruneBatchSize` | 1000 | `housekeeping.go:39` |
| `housekeepingBatchSize` | 4 | `maintenance.go:60` |
| `maintenanceSampleInterval` | 15 s | `maintenance/metrics.go:37` |
| `maintenanceQueryTimeout` | 2 s | `metrics.go:38` |
| `maintenanceStaleAfter` | 60 s | `metrics.go:39` |
| `maintenanceDriftLookback` / `maintenanceDriftLimit` | 24 h / 1000 | `metrics.go:40-41` |
| `maintenanceMetricsMaxRequestsInFlight` | 2 | `metrics.go:35` |
| `maintenanceMetricsMaxHeaderBytes` | 64 KiB | `metrics.go:34` |

The egress block carries its own rationale in source: "a webhook target is a third-party endpoint,
and every relaxation here is a way for a caller to reach something inside the fleet's own network."
Treat those six values as a security boundary, not a tuning surface.

## Instrumentation reality

The tree has exactly **two** Prometheus collectors, and neither belongs to the seven roles that
have no objective.

`internal/providers/admissionmetrics/metrics.go:99-111` — the admission role, two families:

- `mindclade_control_admission_decisions_total`
- `mindclade_control_admission_decision_duration_seconds`

`internal/providers/maintenance/metrics.go:372-384` — the **maintenance** role, seven families.
They carry the `mindclade_control_admission_` prefix because they describe admission *data*, not
because admission emits them:

- `..._expiration_backlog`
- `..._oldest_expired_reservation_age_seconds`
- `..._last_successful_sweep_timestamp_seconds`
- `..._consecutive_backlogged_sweeps`
- `..._event_drift`
- `..._snapshot_success`
- `..._snapshot_last_success_timestamp_seconds`

Maintenance also stands up its own bounded metrics HTTP server (`metrics.go:28-35`), so it has a
scrape surface as well as indicators. **It is the best-instrumented role in the tree** — which
matters, because this document is its SLO.

Health and readiness surfaces exist in exactly two files:
`internal/providers/registry/serving.go` and `internal/providers/api/serving.go`.

**The remaining roles emit nothing.** `scheduler`, `controller`, `operator`, `event_projector`,
`event_dispatcher`, `webhook_dispatcher` and `ingestion_controller` have no collector, no health
surface, and therefore no indicator an objective could be measured against. Instrumentation is a
prerequisite for their objectives, not a follow-up to them.

## Unratified candidates

> **Unratified candidates — not agreed targets.** Every value below is derived mechanically from
> a bound already enforced in source, cited inline. `platform-control` must ratify, amend, or
> reject each one. **No gate may cite these values as evidence,** and no dashboard or alert should
> be built on them until ratified.

| Candidate indicator | Candidate value | Derived from |
| --- | --- | --- |
| Leadership failover completes within | 20 s | `leaseTTL` 15 s + `leaseAcquireInterval` 2 s, plus margin (`scheduler.go:49,51`) |
| Lease release does not exceed | 5 s | `leaseReleaseTimeout` (`scheduler.go:52`) |
| Webhook delivery attempt bounded at | 20 s | `egressTimeout` (`webhook.go:59`) |
| Abandoned outbox claim becomes reclaimable within | 30 s | `dispatcherClaimDuration` (`dispatcher.go:29`) — this bounds stall recovery, **not** drain latency, which is governed by the 250 ms poll and the 64-item batch |
| Maintenance metric staleness alarm at | 60 s | `maintenanceStaleAfter` (`metrics.go:39`) |
| Terminal record retention | 7 d | `expirationTerminalRetention` (`housekeeping.go:38`) |

These are the shape of an objective, not its substance. A bound is what the code refuses to
exceed; an objective is what operators promise, and only `platform-control` can make that promise.

## Correctness invariants (release-blocking, not traded for availability)

These hold regardless of any objective and have a zero error budget:

- No role starts without entering through `bootstrap.Main`; `UnconfiguredFactory` is forbidden in
  promoted commands.
- An unconfigured seam fails closed and never reports successful work.
- Singleton work loops run only under a held lease; a lost lease stops the loop rather than
  reusing a cancelled one.
- Bounded queues, batches, and retention windows are never converted into unbounded buffers to
  absorb load.
- Availability is never restored by bypassing admission or authorization.

## What `platform-control` must decide

This document cannot be completed without the following, none of which is derivable from source:

1. Whether the process-level availability objective is stated per role or per deployment.
2. The measurement window and error-budget policy.
3. Whether leadership failover is an availability event or an expected transition.
4. Which roles require instrumentation before promotion, and in what order.
5. Ratification, amendment, or rejection of each unratified candidate above.

Until items 1–3 are answered, `services.control_plane` may not advance past `implemented`.
The `registry` role is qualified in its own right (`docs/qualification/go/control-plane-registry.md`),
but the deployable does not inherit that while ten of its eleven roles are held fail-closed.
