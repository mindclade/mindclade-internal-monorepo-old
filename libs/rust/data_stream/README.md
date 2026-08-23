# `mindclade_data_stream`

Deterministic rank-local shard plans, topology-bound resume cursors, verified object
reads, bounded prefetch queues, and injected retry behavior. Dataset semantics and
mixtures remain in `data/` and `training/`.

`AsyncPrefetcher` performs bounded parallel blocking fetches while preserving
plan order. Its concurrency limit remains held until a blocking provider call
actually returns, including after an async timeout, and the sum of in-flight
plus reorder-buffered shards never exceeds configured capacity. Explicit
shutdown is deadline-bounded; `Drop` cancels/aborts without joining potentially
blocking provider I/O. The legacy synchronous `Prefetcher` remains for callers
that cannot run Tokio and should not be used on async request paths.

A provider-supplied `RetryHint::After` is untrusted input — for a network object
store it is a remote `Retry-After` — so both retry loops clamp it to the
policy's `maximum_delay`. Retry is therefore bounded in total duration as well
as in attempts, and a peer cannot decide how long a node stalls with a shard
occupying its in-order reorder buffer.
