# Training worker

**Language:** Python/PyTorch with Rust node/checkpoint support  
**Status in this archive:** target-state adapter scaffold; not production-ready.

## Role

Runs the authoritative trainer, task, state, topology, optimizer, checkpoint semantics, telemetry, and safe points.

## Boundary

The worker is a deployable adapter. Reusable semantics live in the owning
`data/`, `preprocessing/`, `models/`, `serving/`, `training/`, or `evaluation/`
package. It consumes immutable inputs and a signed execution ticket, reports
versioned status, commits outputs atomically by digest/manifest, and honors
cancellation, deadline, fencing, retry, and drain.

It does not own:

- cluster quota/scheduling;
- checkpoint byte transfer/storage authority;

## Required operational behavior

- validate ticket, bundle, artifact scope, attempt, and fencing before work;
- reserve bounded CPU/RAM/disk/GPU/process/output resources;
- make stage retries explicit and idempotent;
- keep control RPCs small and place bulk data in files/shared memory/object refs;
- preserve deterministic seeds/config/policy/reference/tool provenance;
- reject late output/status commits after cancellation or claim loss;
- emit bounded diagnostics and release resources during drain.

## Limitations

The checked-in source is an ownership/build scaffold. The actual engine,
provider adapters, connected tests, performance limits, and qualification
remain to be implemented before promotion.
