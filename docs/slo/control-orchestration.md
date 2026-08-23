# control.orchestration SLO

- Availability and latency/error objectives are defined before production promotion.
- Correctness invariants are release-blocking and are not traded for availability: no commit
  accepted under a stale fencing token, attempt-budget accounting that never grants an attempt
  beyond the declared budget, and workflow definition digests that are deterministic for a given
  compiled plan.
- Terminal stage and attempt transitions are forward-only; a late worker must never reopen or
  overwrite a settled outcome.
- Measurements and exclusions must be retained as release/incident evidence.
