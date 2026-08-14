# Runbook: data ingestion stalled

## Trigger

A source snapshot, transfer, parse, canonicalization, curation, quality, shard,
or publication stage stops progressing beyond its configured budget.

## Triage

1. Identify source, immutable source snapshot, cursor, stage ID, work item,
   attempt, owner, fencing token, and last heartbeat.
2. Determine whether the fault is in Go workflow state, Rust transfer/parser,
   Python scientific stage, provider storage, reference source, or publication.
3. Check queue depth, lease age, claim renewals, retry/dead-letter state, disk,
   memory, network, object-store requests, and local cache capacity.
4. Verify the worker still holds the current claim before accepting status or
   artifacts.

## Recovery

- Expired/stale claim: allow fenced reclamation; reject late completion.
- External source unavailable: retain the immutable snapshot/cursor, apply
  bounded backoff, and avoid advancing the source cursor.
- Transfer/parser failure: retry from a verified byte offset or immutable raw
  artifact; quarantine malformed inputs rather than weakening strict limits.
- Python curation failure: preserve raw/canonical artifacts and rerun the
  scientific stage under a new policy/version.
- Publication failure: verify all shard manifests and resume the atomic dataset
  publication transaction/outbox path.

## Exit criteria

The cursor advances monotonically, the stage either completes or reaches an
explicit terminal state, no duplicate publication occurs, lineage references
immutable artifacts and policies, and backlog/age return within SLO.
