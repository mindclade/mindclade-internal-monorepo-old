# Curation worker

**Language:** Python scientific engine under Rust supervision
**Status:** implemented adapter; connected engine and deployment qualification pending.

This composition root adapts the unified stage-worker protocol to scientific normalization, deduplication, and dataset construction. It uses
`libs/python/worker_runtime` for immutable envelopes, cooperative deadlines/cancellation,
non-blocking concurrency admission, and exact drain accounting. Go retains durable DAG,
attempt, retry, and publication state; `libs/rust/{worker_protocol,worker_runtime,python_bridge}`
retains ticket verification, fencing, process/resource supervision, and bulk-buffer transport.

The owning scientific engine is injected and must checkpoint at safe interruption points. This
adapter does not load provider credentials, construct a scheduler, verify signatures in Python,
or publish artifacts. Output publication remains atomic and fencing-aware in the Rust/Go path.

Hard local concurrency and drain ceilings are represented by `WorkerLimits`. The adapter rejects
work before readiness, during drain, after deadline, for another stage kind, or when local
concurrency is exhausted.
