# Runbook: GPU health degradation

## Trigger

ECC/Xid errors, device loss, thermal/power throttling, allocator failures,
collective errors, repeated worker crashes, or unexplained latency/throughput
regression occurs on a GPU node.

## Immediate actions

1. Stop local admission and mark the runtime host/node unready.
2. Drain new requests and stop scheduling new training/inference work.
3. Preserve GPU, driver, runtime, kernel, process, memory, topology, and node
   diagnostics.
4. Cancel or preempt work only through the owning runtime/control contract so
   checkpoints, artifact commits, and fencing remain valid.

## Diagnosis and recovery

- Distinguish hardware faults from kernel/compiler, model memory, NCCL/RDMA,
  driver, and host-resource faults.
- Verify qualified kernel signature and fall back to the PyTorch reference when
  a TileLang/provider regression is suspected.
- For distributed training, coordinate rank failure and restore from the last
  verified checkpoint; do not restart a single rank outside the engine policy.
- Quarantine the node on persistent hardware/driver faults and require a clean
  qualification burn-in before return.

## Exit criteria

The node passes device, memory, kernel, communication, cancellation, and
shutdown smoke tests; no stale process owns GPU resources; and the scheduler has
received the reconciled capacity state.
