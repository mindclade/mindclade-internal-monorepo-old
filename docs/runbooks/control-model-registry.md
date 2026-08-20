# Runbook: control model registry degraded

## Trigger

- Registry availability or latency burns its SLO budget.
- `/readyz` fails because PostgreSQL or a required provider is unavailable.
- Model resolution reports a digest mismatch or stored-document corruption.
- Release promotion rolls back, conflicts on resource version, or leaves an
  invariant alert.
- Authentication succeeds but authorization mapping is missing or denied at
  an unexpected rate.

## Immediate actions

1. Freeze model publication and release promotion; reads may continue while
   their sealed content verifies.
2. Record the affected image digest, source revision, configuration digest,
   request IDs, registry role, database instance, and migration version.
3. Separate provider health: PostgreSQL connectivity/locks, object storage,
   cache, listener/probes, authentication, then permission mapping.
4. Do not bypass the domain service with direct SQL. A release promotion must
   commit its evidence graph and release record in one serializable
   transaction.
5. Preserve a corrupt row and its sealed digest as incident evidence; do not
   overwrite content-addressed descriptors.

## Recovery

- Restore PostgreSQL authority and run the registry-owned append-only
  migrations before reopening mutations.
- For a transient database outage, allow bounded retry only after readiness is
  healthy. Confirm retry exhaustion remains visible rather than acknowledged.
- For a resource-version conflict, re-read the release and repeat policy
  evaluation; never blind-retry a stale compare-and-swap.
- For descriptor corruption, restore the immutable row from a verified backup
  or republish the original sealed descriptor under the same digest only when
  its canonical bytes match exactly.
- For a bad release, follow [release rollback](release-rollback.md), verify the
  signed last-known-good digest and provenance, then update the reviewed GitOps
  digest pin.

## Verification and exit criteria

- Run the live PostgreSQL registry suite and the control-plane failure matrix.
- Resolve a known descriptor twice and confirm the same digest and body.
- Promote a disposable qualified release and confirm graph/release atomicity;
  inject a failure after graph write and confirm zero partial rows.
- Confirm stale lease owners cannot renew, authorization mappings cover every
  mounted route, probes are healthy, and SLO burn has recovered.
- Attach the evidence and corrective action to the incident before unfreezing
  promotion.
