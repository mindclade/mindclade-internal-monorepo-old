# control.scheduling runbook

## Trigger

Workloads stop being admitted, quota accounting diverges from observed reservations, accelerator
capacity is held without progress, or a scheduling rollout must be stopped mid-incident.

## Immediate actions

1. Record the affected queue identity: LocalQueue, ClusterQueue, cohort, workload names, nominal
   and borrowed quota, and the reservations currently charged against each flavor.
2. Stop admission by holding the queue, not by deleting workloads. Deleting an admitted workload
   drops the reservation bookkeeping on the floor and makes quota conservation unverifiable.
   Use `stopPolicy: Hold` for a graceful stop and `stopPolicy: HoldAndDrain` for an incident that
   requires evicting running workloads.
3. Remember that Kueue nominal quotas are held at zero by default in this repository. A queue
   admitting nothing is the configured state, not automatically a fault — confirm the intended
   quota before treating zero admission as a regression.
4. Verify that no GPU slot is reserved while its upstream preprocessing stage is still pending.
   A reservation ahead of its dependency is a correctness defect, and it is the first thing to
   check when accelerators are idle but capacity reads as full.
5. Preserve the queue and capacity projection snapshot as incident evidence.

## Recovery

Release the hold only after quota accounting reconciles: charged reservations must equal the
admitted set, with no orphaned charges from evicted or deleted workloads. Restore nominal quota
in the intended order so fair-share ordering stays deterministic for the queue state being
replayed. During a rollback, never remove the Kueue CRDs — removing them deletes every queue and
workload object the control plane reconciles against, converting a reversible configuration
rollback into unrecoverable state loss. Roll back the controller or configuration instead.

## Exit criteria

Quota is conserved across admission, reservation, and release; no accelerator reservation exists
without a satisfied upstream stage; queues are unheld with their intended nominal quota; fair-share
ordering is reproducible for the recorded queue state; and the admission stall is recorded against
the service SLO.
