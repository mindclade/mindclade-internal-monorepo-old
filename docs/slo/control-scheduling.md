# control.scheduling SLO

- Availability and latency/error objectives are defined before production promotion.
- Correctness invariants are release-blocking and are not traded for availability: quota
  conservation across admission, reservation, and release; deterministic fair-share ordering for
  a given queue state; and no accelerator reservation held while an upstream preprocessing stage
  is still pending.
- Placement decisions are bounded and idempotent per workload; a retry must not double-charge
  quota or double-reserve a slot.
- Measurements and exclusions must be retained as release/incident evidence.
