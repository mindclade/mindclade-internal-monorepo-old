# Runbook: checkpoint failure

## Scope and activation state

This procedure covers manifest-last reference-training checkpoint publication and the bounded DCP
restore claim. The metric producer remains the unimplemented external contract in
`infra/observability/training-metrics.json`; the Cloud Monitoring alert is disabled, and its
source ratio is an explicitly proposed, unapproved value. Environment owners must supply any
accepted threshold, SLO, cost target, RPO, and RTO.

World-eight H100 restores are same-world only. Replicated portability is limited to world one and
world two; this runbook does not claim general resharding, FSDP, or world-eight cross-size restore.

## Checkpoint terminal failure ratio

Checkpoint save, staging, upload, manifest commit, restore, or verification fails or exceeds its
environment-approved pause, memory, or transfer envelope.

## Failure matrix

| Phase or evidence | Failure class | Containment | Safe recovery and proof |
|---|---|---|---|
| DCP planning or tensor write fails before manifest | Local serialization | Preserve the previous committed checkpoint; discard only uncommitted attempt state | Retry from the same safe point and prove every artifact digest before commit |
| Transfer fails or object digest mismatches | Provider/transport or corruption | Fence the attempt and protect verified objects from overwrite | Resume verified parts through the provider-owned transfer path; follow `artifact-corruption.md` on mismatch |
| Manifest publication fails | Atomic commit failure | Treat the attempt as nonexistent; never update the registry pointer | Re-verify all objects, publish manifest last, then independently read it back by digest |
| Registry publication fails after manifest commit | Lineage/control-plane failure | Preserve the immutable committed manifest and block promotion | Retry the idempotent registry operation with the same identity and prove no duplicate checkpoint |
| Restore returns `incompatible` | Schema/topology contract mismatch | Do not coerce, partially load, or rewrite source state | Use a supported same-world or declared replicated portability path, or fall back to the last compatible checkpoint |
| Restore returns `failed` or non-exact state | Corruption/numerical failure | Hold the run and every dependent release artifact | Validate all digests, counters, cursor, RNG contract, and learned weights before resumption |
| Checkpoint metric inventory absent or oversized | Exporter contract failure | Keep the checkpoint alert disabled and hold qualification evidence | Restore exact 73-series inventory and replay/idempotence evidence before fire/resolve testing |

## Triage

1. Classify the phase: Python/DCP state planning, CPU staging, provider transfer, object storage,
   manifest commit, registry publication, or restore.
2. Preserve run ID, step, topology fingerprint, checkpoint request ID, node/rank diagnostics,
   staging budget, and last successfully committed checkpoint outside telemetry labels.
3. Keep training on the last safe policy: continue without checkpoint only when the approved
   durability window permits it; otherwise stop at a safe point.
4. Never publish a checkpoint until all objects verify and the atomic manifest commit succeeds.
5. Correlate publication outcomes, last-commit age, restore outcomes, active phase, and canonical
   checkpoint/run identities. Never put checkpoint or run IDs in metric labels.

## Recovery

- Failed in-flight save: discard uncommitted staging objects and retry from the same safe training
  state if replay is supported.
- Host-memory pressure: reduce staging concurrency/chunk size or switch to the approved synchronous
  emergency policy; account for checkpoint bytes in the node-wide budget.
- Transfer/provider failure: resume verified parts with the provider-owned transfer path; do not
  make Python own provider retry loops.
- Restore failure: validate schema, component registry, topology compatibility, object digests,
  and the bounded portability claim; fall back to the last verified checkpoint.
- Corrupt object: follow `artifact-corruption.md`.

## Non-production synthetic fire and resolve

1. Record the approved non-production project/cluster, policy and exporter digests, channel
   resources, runbook URL, approved ratio/minimum-sample disposition, and immutable test cohort.
2. Prove exporter target health, exact family inventory, recording-rule health, and an initially
   healthy committed-publication ratio.
3. Through the reviewed synthetic-event seam, emit bounded committed and failed publication
   outcomes until the configured sample guard is satisfied and the environment-approved ratio is
   exceeded for its approved duration. Use only fixed phase/result labels.
4. Confirm one notification reaches each non-production channel with the correct runbook; retain
   the evaluated numerator, denominator, time window, and policy digest.
5. Emit sufficient successful synthetic outcomes for the ratio to recover, confirm both channels
   receive resolution, and verify restore-exact diagnostics remain internally consistent.
6. Remove synthetic state, verify no series/cardinality leak, disable the policy, and store the
   fire/resolve and final-clean-state evidence.

Stop if missing data fires unexpectedly, the denominator differs from terminal attempts, any
forbidden label appears, or resolution cannot be demonstrated. Never inject failures into a real
checkpoint bucket or production notification channel.

## Rollback and containment

Hold new H100 admission and stop the affected run at a safe point through its owning orchestration
path. Preserve the last verified manifest and all referenced immutable objects, fence the failed
attempt, and restore the prior qualified trainer/checkpoint-agent/manifest tuple. Once no active
work consumes the capacity, restore the zero Kubernetes and Kueue quotas through reviewed GitOps.
Do not delete a committed manifest, retarget a registry pointer to unverified state, remove a
finalizer, or repair corruption in place.

## Exit criteria

A checkpoint is atomically committed and independently restorable, or the run is terminated with
the last verified checkpoint protected. Evidence includes manifest, digests, topology,
code/toolchain provenance, state registry, restore result, and bounded fire/resolve telemetry.
