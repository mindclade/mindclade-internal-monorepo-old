# Admission metrics provider

This package instruments the control-plane admission engine and owns the API
role's dedicated Prometheus listener. It does not query PostgreSQL, reconcile
audit or outbox state, or own Kubernetes scrape configuration.

The listener binds `MINDCLADE_METRICS_ADDRESS`, serves exactly unauthenticated
`GET /metrics` and `HEAD /metrics`, and is registered as a `servicekit`
component. Startup fails closed when its address cannot be bound. Requests,
headers, and response generation are time-bounded, headers are size-bounded,
and request bodies are never read by this endpoint.

Its private Prometheus registry exports only:

- `mindclade_control_admission_decisions_total{operation,result}` where
  `operation` is `admit`, `commit`, or `release` and `result` is `allow`,
  `deny`, `exhausted`, `conflict`, `not_found`, `unavailable`, `internal`,
  `deadline`, or `invalid`;
- `mindclade_control_admission_decision_duration_seconds{operation}` as a
  histogram of actual engine call duration.

All 27 decision combinations are initialized to zero so a missing series means
a missing scrape rather than merely no traffic. Tenant, workspace, subject,
model, provider, route, reason, request, reservation, and idempotency values are
never metric labels. The registry intentionally omits Go and process collectors
because this endpoint's bounded source contract is admission decisions only.

Successful calls are `allow`; permission failures are `deny`; capacity failures
are `exhausted`; version, idempotency, and precondition failures are `conflict`;
missing reservations are `not_found`; dependency failures are `unavailable`;
cancellation and deadline expiration are `deadline`; validation and range
failures are `invalid`; and unknown, data-loss, unimplemented, or recovered
panic paths are `internal`. Panics remain visible to the canonical HTTP recovery
boundary after the metric is recorded.

Unit and race tests qualify taxonomy, cardinality, elapsed duration, concurrent
recording, method restrictions, listener conflict, and lifecycle shutdown.
Connected scrape, alert, dashboard, and production-SLO evidence remain separate
activation gates.
