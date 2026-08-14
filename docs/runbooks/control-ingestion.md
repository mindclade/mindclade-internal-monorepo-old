# Ingestion control degradation

1. Stop creation of new source snapshots if cursor or artifact provenance is uncertain.
2. Preserve already committed immutable raw artifacts.
3. Verify the latest source cursor and fencing generation before retrying a stage.
4. Reconstruct a `WorkloadEnvelope` only from committed run/job/stage state and a fresh execution ticket.
5. Never bypass artifact digest verification or reuse a stale ticket to accelerate recovery.
