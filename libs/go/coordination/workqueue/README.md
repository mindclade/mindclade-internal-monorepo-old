# Durable work queue

`workqueue` provides bounded, fenced leasing for long-running Go control-plane
work. Producers enqueue immutable work descriptions; workers claim with a token
and version, renew heartbeats, cancel handlers on claim loss, and transition to
complete, retry, or dead-letter.

Use it for schedulers, controllers, operators, ingestion stages, webhook jobs,
maintenance, and other independently retryable work. Do not use it for ordered
event projection (use inbox + cursor) or transactional publication (use
outbox).

Register the worker component through `servicekit/production` so drain stops
new claims before in-flight work is cancelled or allowed to finish within the
shutdown budget.
