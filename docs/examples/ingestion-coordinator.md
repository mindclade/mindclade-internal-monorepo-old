# Integration example: ingestion coordinator

The ingestion coordinator is a Go durable-policy process around Rust byte
workers and Python scientific curation.

Its narrow mechanism contract is `control/ingestion.Mechanisms`:

```text
clock and identifiers
audit and idempotency
retry and transactions
blob and cache stores
leases and durable cursors
fenced work queue
transactional outbox and messaging
signing and resource versions
```

The process performs source-snapshot discovery, creates immutable work items and
execution tickets, tracks durable stages and cursors, and publishes state
transitions. Rust workers fetch and verify bytes; Python workers canonicalize
and curate biological records. Scientific semantics never enter `libs/go`.

The `ingestion-controller` command uses the same bootstrap as every other Go
process. Its production role additionally requires blob/cache, cursor,
work-queue, lease, messaging, signing, and outbox capabilities. A provider
factory supplies concrete stores and the domain coordinator component; the
shared lifecycle owns claims, drain, cancellation, and shutdown.

Relevant source:

```text
services/control_plane/cmd/ingestion_controller/main.go
services/control_plane/internal/bootstrap/
control/ingestion/foundation.go
libs/go/coordination/cursor/
libs/go/coordination/workqueue/
libs/go/coordination/outbox/
libs/go/storage/blob/
libs/go/storage/cache/
```
