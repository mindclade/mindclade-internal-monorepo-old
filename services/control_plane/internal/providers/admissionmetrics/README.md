# Admission metrics provider

This package observes the control-plane admission HTTP boundary and owns the
API role's dedicated Prometheus listener. The admin role does not construct or
mount it. It does not query PostgreSQL, reconcile audit or outbox state, or own
Kubernetes scrape configuration.

The listener binds `MINDCLADE_METRICS_ADDRESS`, serves exactly unauthenticated
`GET /metrics` and `HEAD /metrics`, and is registered as a `servicekit`
component. Startup fails closed when its address cannot be bound. Requests,
headers, and response generation are time-bounded, headers are size-bounded,
and request bodies are never read by this endpoint.

Its private Prometheus registry exports only:

- `mindclade_control_admission_decisions_total{operation,result}` where
  `operation` is `admit`, `commit`, or `release` and `result` is `allow`,
  `deny`, `exhausted`, `conflict`, `not_found`, `unavailable`, `internal`,
  `deadline`, `canceled`, or `invalid`;
- `mindclade_control_admission_decision_duration_seconds{operation}` as a
  histogram of qualified request time from guarded HTTP entry through terminal
  response completion.

All 30 decision combinations are initialized to zero so a missing series means
a missing scrape rather than merely no traffic. Tenant, workspace, subject,
model, provider, route, reason, request, reservation, and idempotency values are
never metric labels. The registry intentionally omits Go and process collectors
because this endpoint's bounded source contract is admission decisions only.

The boundary clock starts before the guarded API stack. A request enters the SLI
only after authentication, authorization, and structural decoding have
succeeded, so their elapsed time is retained for qualified requests while
unauthenticated and malformed requests discard the clock. Semantic validation,
engine execution, error rendering, successful serialization, and terminal
response writes are all inside the qualified interval. The observer records
once after the stack returns and de-duplicates accidental nested installation.

Successful responses are `allow`; permission failures are `deny`; capacity
failures are `exhausted`; version, idempotency, and precondition failures are
`conflict`; missing reservations are `not_found`; dependency and response-write
failures are `unavailable`; validation and range failures are `invalid`; and
unknown, data-loss, unimplemented, status/fault contract mismatches, or panic
paths are `internal`. The boundary combines the handler's bounded fault category
with the terminal HTTP status; a failed response write supersedes a prior
semantic outcome.

Caller cancellation is `canceled`, distinct from a server `deadline`. Canceled
requests increment their bounded decision counter for operational evidence but
do not contribute a latency sample and must be excluded from both the SLI
numerator and denominator (`result!="canceled"`). Server deadlines remain
availability failures and retain their latency sample.

The response observer does not inspect or buffer bodies. It preserves the exact
optional-interface set and forwards `http.Flusher`, `http.Hijacker`,
`http.Pusher`, and `io.ReaderFrom` behavior when the underlying writer supports
each one. It exposes `Unwrap` while tracking only first status and write-failure metadata.
Panics are observed as `internal` without being swallowed or replaced by
telemetry behavior.

Unit, integration, and race tests qualify taxonomy, fixed cardinality, parsing
and serialization latency, cancellation and deadline behavior, terminal status
and write failure, concurrent recording, method restrictions, listener
conflict, and lifecycle shutdown.
Connected scrape, alert, dashboard, and production-SLO evidence remain separate
activation gates.
