# Durable work queue

`workqueue` provides bounded, fenced leasing for long-running Go control-plane
work. Producers enqueue immutable work descriptions; workers claim with a token
and version, renew heartbeats, cancel handlers on claim loss, and transition to
complete, retry, or dead-letter. Stores also provide bounded, queue-scoped
terminal-record pruning. Retention callers must choose an explicit cutoff;
pending and leased records are never eligible for deletion.

A terminal record is also the queue's duplicate-ID tombstone. Once pruned, a
delayed producer can enqueue that ID again. The retention horizon must therefore
exceed every producer retry/replay horizon, or the handler's external effect
must be independently idempotent.

An item's validated request metadata is restored into the handler context on
every attempt, preserving request, correlation, causation, and operation
lineage across durable queue boundaries.

Use it for schedulers, controllers, operators, ingestion stages, webhook jobs,
maintenance, and other independently retryable work. Do not use it for ordered
event projection (use inbox + cursor) or transactional publication (use
outbox).

Register the worker component through `servicekit/production` so drain stops
new claims before in-flight work is cancelled or allowed to finish within the
shutdown budget.
