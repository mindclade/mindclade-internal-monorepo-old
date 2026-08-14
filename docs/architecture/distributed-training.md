# Distributed training

## Topology authority

Model families declare parameter/activation semantics and a declarative
parallel plan. Training compiles that plan into device meshes, process groups,
placements, pipeline stages, expert groups, replica groups, activation
checkpointing, precision, and communication schedules.

Supported dimensions include data parallelism, FSDP2, tensor parallelism,
pipeline parallelism, context/sequence parallelism, expert parallelism, and
hybrid multi-dimensional plans.

## Control and execution

```text
Go scheduler/Kueue
  -> admits job and topology resource request
Kubernetes JobSet
  -> creates coordinated Pods and placement constraints
Rust node agent
  -> stages artifacts, monitors resources, handles transfer/preemption
Python training runtime
  -> initializes groups, executes collectives and numerical state
```

## Correctness

- collective schedule and topology fingerprints are recorded;
- model parallel plans are validated before process-group creation;
- metrics/losses specify exact reduction groups and dtypes;
- checkpoint manifests contain topology and layout metadata;
- world-size changes are tested through load-time resharding;
- callbacks cannot call collectives; hooks declare execution scope;
- failure detection distinguishes node/process/collective/data/checkpoint faults.

## Recovery

Preemption triggers a bounded safe-point/checkpoint attempt. Restart uses the
latest committed checkpoint only. Stale attempts are fenced from status or
artifact commit. Rendezvous and restart policy are explicit engine/runtime
capabilities, not hidden in the model.

## Qualification

Release lanes cover single-device parity, sharded/unsharded parity, topology
changes, gradient/loss reductions, checkpoint resume, fault injection,
straggler/deadlock diagnostics, scale efficiency, MFU, communication overlap,
and recovery time objectives.
