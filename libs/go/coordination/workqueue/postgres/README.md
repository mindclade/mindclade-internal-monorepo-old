# PostgreSQL work queue

Uses `FOR UPDATE SKIP LOCKED` and fenced transitions. Terminal retention is a
bounded, queue-scoped `DELETE` over completed, failed, and cancelled records;
locked records are skipped and active work is never selected.

Pruning also removes duplicate-ID tombstones. A queue owner must retain them
past its producer replay horizon or prove independently idempotent effects.

`TerminalRetentionDDL` declares the supporting partial
`(queue, completed_at, item_id)` index separately from the original table DDL.
Schema owners must append it as a new checksummed migration; changing an
already-applied table migration is forbidden.
