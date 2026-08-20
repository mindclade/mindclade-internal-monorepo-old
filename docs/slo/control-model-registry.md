# Control model registry SLO

**Owner:** platform-control
**Criticality:** tier-1
**Measurement window:** rolling 30 days

## Service-level indicators

- **Availability:** successful authenticated registry requests divided by all
  authenticated, policy-authorized requests. Caller errors (`4xx` other than
  `408` and `429`) are excluded; authorization denials remain security signals.
- **Model resolution latency:** server duration for
  `GET /v1/registry/models/{digest}`.
- **Mutation latency:** server duration for model publication and release
  promotion, including the PostgreSQL commit.
- **Durability/correctness:** a committed descriptor or promoted release is
  resolvable by its sealed digest; no partial evidence graph/release pair,
  stale-fence commit, or unauthorized mutation is permitted.

## Objectives

- Availability: **99.9%** per rolling 30 days.
- Model resolution: **99% under 250 ms** and **99.9% under 1 s**.
- Publication and promotion: **99% under 1 s** and **99.9% under 5 s**.
- Recovery: **RPO 0** for acknowledged PostgreSQL commits and **RTO 60
  minutes** for a regional control-plane restoration.
- Correctness and authorization invariants have a zero-error objective and are
  release blocking; they are never traded against availability.

## Alerting and evidence

- Page on a 5-minute availability burn of 14.4x or a 1-hour burn of 6x.
- Ticket on a 6-hour burn of 3x or a 3-day burn of 1x.
- Page immediately on digest mismatch, partial-promotion detection,
  authorization-mapping failure, migration failure, or stale-fence commit.
- Retain dashboards, alert transitions, live-PostgreSQL qualification, failure
  injection, and rollback verification with the release evidence bundle.

Planned maintenance is not silently excluded. Any exclusion requires an
incident or change record naming the interval, owner, and customer impact.
