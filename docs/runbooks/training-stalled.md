# Runbook: training stalled

## Trigger

No committed training-step progress, repeated collective timeouts, data
starvation, checkpoint backpressure, rank failure, deadlock warning, or severe
straggler behavior exceeds the configured watchdog threshold.

## Triage

1. Capture run, step, topology/mesh, rank/world state, collective schedule
   digest, data-loader position, optimizer/state phase, checkpoint activity,
   GPU/NCCL/RDMA health, and stack/flight-recorder diagnostics.
2. Distinguish numerical work, data, compilation, collective, checkpoint,
   callback/telemetry, node, and orchestration causes.
3. Do not advance durable step state until the authoritative training
   transaction/safe-point contract commits.

## Recovery

- Data starvation: inspect Rust stream worker/object store and loader queues.
- Collective/rank failure: use engine resilience policy; do not independently
  restart one rank outside the topology contract.
- Node loss: follow `node-preemption.md` and restore from verified checkpoint.
- Checkpoint backpressure: follow `checkpoint-failure.md`.
- Numerical failure: stop, preserve state/inputs/RNG, and run deterministic
  qualification; do not silently skip the step.

## Exit criteria

The run resumes from a known safe state with monotonic progress or terminates
cleanly with a verified checkpoint and diagnostic bundle. No duplicate step,
optimizer update, or artifact publication occurred.
