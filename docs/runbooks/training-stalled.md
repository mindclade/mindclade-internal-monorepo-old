# Runbook: training stalled

## Scope and activation state

This runbook applies only to the bounded `reference-affine-v1` qualification phases
`h100-1g-smoke` and `h100-8g-ddp-dcp`. The metric producer is the unimplemented external
contract in `infra/observability/training-metrics.json`. Alerts are disabled and their source
thresholds are proposed, not approved. Do not infer a live incident path until connected GMP
scrape, rule, notification, and fire/resolve evidence exists.

No SLO, cost target, RPO, or RTO is defined here. The incident commander uses only values approved
for the named environment and immutable qualification cohort.

## Training progress stalled

No committed training-step progress, repeated collective timeouts, data starvation, checkpoint
backpressure, rank failure, deadlock warning, or severe straggler behavior exceeds the configured
watchdog threshold.

## Failure matrix

| Evidence | Classification | Immediate containment | Recovery and proof |
|---|---|---|---|
| Metric-contract status absent or not `1` | Exporter/scrape contract failure | Keep alert policy and H100 queue disabled; do not interpret missing progress as workload health | Restore the external producer, exact 73-series inventory, target health, and rule evaluation before retesting |
| Active runs are non-zero but the oldest-progress timestamp is absent or zero | Producer correctness failure | Hold new admission and preserve canonical run events | Replay into fresh bounded exporter state and prove idempotent counters and timestamps |
| Optimizer-step increase is zero while data and GPU activity continue | Numerical, collective, or commit-path stall | Stop at the next safe point; never advance the durable step counter | Compare all ranks, global denominator, finite-gradient state, and exact topology; resume only from verified state |
| Data queues empty before committed progress | Data starvation | Hold the affected run without changing dataset/config identity | Restore the immutable input source and prove cursor and lineage continuity |
| Checkpoint age grows with publication failures | Checkpoint backpressure | Follow `checkpoint-failure.md`; preserve the last committed manifest | Prove manifest-last publication and exact restore before resuming |
| JobSet fails with worker, collective, or deadline reason | Runtime or topology failure | Hold the LocalQueue and preserve Pod/JobSet conditions and rank diagnostics | Recreate only through the owning orchestration path using the same cohort or record a new cohort |
| GPU/XID/NCCL/RDMA evidence indicates node failure | Hardware or transport failure | Isolate the run; do not restart one rank independently | Follow `gpu-health.md`, then restore all ranks from the same verified checkpoint |

## Triage

1. Capture run, step, topology/mesh, rank/world state, collective schedule digest, data-loader
   position, optimizer/state phase, checkpoint activity, GPU/NCCL/RDMA health, and bounded
   stack/flight-recorder diagnostics.
2. Distinguish numerical work, data, compilation, collective, checkpoint, callback/telemetry,
   node, and orchestration causes.
3. Do not advance durable step state until the authoritative training transaction/safe-point
   contract commits.
4. Compare active runs, optimizer-step increase, progress age, terminal outcomes, JobSet
   conditions, and checkpoint signals for the same fixed phase. Metric labels must never include
   run, checkpoint, model, dataset, tenant, or request identity.

## Recovery

- Data starvation: inspect the immutable input source and loader queues.
- Collective/rank failure: use engine resilience policy; do not independently restart one rank
  outside the topology contract.
- Node loss: follow `node-preemption.md` and restore from a verified checkpoint.
- Checkpoint backpressure: follow `checkpoint-failure.md`.
- Numerical failure: stop, preserve state/inputs/RNG, and run deterministic qualification; do not
  silently skip the step.

## Non-production synthetic fire and resolve

This is an evidence procedure, not authorization to mutate a cluster.

1. Record the approved non-production project, cluster, namespace, policy digest, external
   exporter digest, channel resource names, runbook URL, and threshold disposition.
2. Prove the exporter target and every recording rule healthy with the exact source profile.
3. Through the exporter's reviewed synthetic-event seam, create one active fixed phase and stop
   advancing its committed progress timestamp. Do not use a real dataset or emit identity labels.
4. Maintain enough samples for the configured minimum and duration. Confirm the semantic
   progress-age signal crosses the environment-approved value and both non-production channels
   receive one notification with the correct runbook link.
5. Advance the synthetic committed progress timestamp and optimizer-step counter. Confirm the
   recording rule recovers and the alert resolves through both channels.
6. Remove synthetic state, verify zero active synthetic runs and no cardinality growth, then
   disable the policy again. Retain fire/resolve timestamps, query output, target/rule health,
   policy and channel digests, and final clean state as immutable evidence.

Stop if any signal is missing, duplicated, carries a forbidden label, cannot resolve, or routes to
a production channel.

## Rollback and containment

Rollback is a reviewed GitOps/orchestration transaction: hold the H100 LocalQueue and
ClusterQueue, suspend new work, let admitted work reach a checkpoint-safe point, and preserve the
last verified checkpoint plus cohort identity. After active use reaches zero, restore the prior
qualified manifest/image digests and the zero ResourceQuota ceilings. Lowering quota alone does
not stop running Pods. Do not create a competing direct field manager, restart one rank, change
the dataset/config identity, or promote a checkpoint produced after the last verified safe point.

## Exit criteria

The run resumes from a known safe state with monotonic progress or terminates cleanly with a
verified checkpoint and diagnostic bundle. No duplicate step, optimizer update, artifact
publication, or telemetry series occurred.
