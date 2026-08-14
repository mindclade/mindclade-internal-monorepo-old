# Slurm orchestration adapter

Translates orchestration requests to a Slurm integration without changing workflow semantics.

The adapter consumes `control/orchestration` plans and shared Go mechanisms. It
does not own scheduling policy, workflow state, scientific/model execution, or
process lifecycle. Production qualification must test idempotent launch,
status reconciliation, cancellation, duplicate delivery, timeout, and stale
attempt/fencing behavior.
