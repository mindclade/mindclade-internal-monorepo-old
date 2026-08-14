# runtime.model_worker SLO

- Availability and latency/error objectives are defined before production promotion.
- Correctness invariants (authorization, fencing, deterministic durable state) are release-blocking and are not traded for availability.
- Measurements and exclusions must be retained as release/incident evidence.
