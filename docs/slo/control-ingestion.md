# Ingestion control SLO

The ingestion control domain is tier-1 durable policy. Accepted source snapshots and stage attempts must be recoverable without duplicate durable effects, cursor regression, or stale-fence commits. Availability is inherited from the control-plane service that hosts the domain; correctness is release-blocking.
