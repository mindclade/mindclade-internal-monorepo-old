# Local orchestration adapter

Runs bounded local development tasks while preserving the same attempt, cancellation, and status contract.

The adapter consumes `control/orchestration` plans and shared Go mechanisms. It
does not own scheduling policy, workflow state, scientific/model execution, or
process lifecycle. Production qualification must test idempotent launch,
status reconciliation, cancellation, duplicate delivery, timeout, and stale
attempt/fencing behavior.
