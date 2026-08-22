# Training worker

**Language:** Python numerical composition root under Rust supervision

**Status:** bounded reference-affine engine implemented; connected deployment qualification
remains disabled.

This package implements one deliberately small conformance workload,
`reference.affine.train.v1`. The affine model is a contract fixture, not a scientific model
family or a claim that the general training umbrellas are complete.

## Authority boundary

The engine accepts an already-admitted `StageEnvelope` and `ExecutionContext`. Rust remains
authoritative for tickets, artifact scope, resource budgets, fencing, process supervision, and
bulk-buffer transport. Go remains authoritative for durable DAG, attempt, retry, and final output
state. Python receives no provider credentials.

`ArtifactIO` is an injected, provider-neutral boundary for authorized immutable reads, verified
checkpoint materialization, and staging immutable bundle/evidence objects. The engine verifies
byte artifacts before use and requires adapters to return the exact expected `ArtifactRef`.

Canonical checkpoint publication is a separate injected `CheckpointCommitter` boundary. Python
builds deterministic `training.v1.CheckpointManifest` semantics over the internal DCP tree. The
committer's `prepare` call may verify and stage components, but is expressly non-authoritative.
Only `commit` may atomically make the canonical manifest and `artifact.v1.CheckpointCommit`
visible, create the Go-owned `registry.v1.CheckpointRecord`, bind the exact four stage outputs,
accept the stage fence/deadline, and transition the stage to `succeeded`. The engine fails closed
when this seam is absent. Objects accepted before a failed terminal commit remain unreachable and
are safe for authority-owned garbage collection.

The canonical manifest includes the adapter-local DCP `manifest.json` as an exact replicated
`TRAINER` component. Resume reads and verifies the admitted canonical protobuf bytes, extracts
that typed inner reference, and makes DCP restoration check the materialized JSON against its
digest. After restore, the worker also requires exact outer/inner equality for counters, cursor,
topology, component refs/semantics, config, dataset, and admitted model/source/toolchain/runtime/
compatibility-policy provenance. A materializer cannot substitute a different self-consistent
tree under the outer ref, and a rehashed outer manifest cannot misstate restored semantics.

## Admitted contract

Inputs are positional and exact:

1. `training.resolved-config`, schema 1,
   `application/vnd.mindclade.training.reference-affine-config.v1+json`
2. `training.dataset`, schema 1,
   `application/vnd.mindclade.training.reference-affine-dataset.v1+safetensors`
3. optional `training.checkpoint.manifest`, schema 1,
   `application/vnd.mindclade.training-checkpoint-manifest.v1+proto`

The resolved config is unique-key UTF-8 canonical JSON with no unknown fields. Floating-point
settings are canonical decimal strings because identity JSON forbids JSON floating-point values.
It pins the affine model/operation and bounds optimizer steps, steps per execution, accumulation,
microbatch size, AdamW settings, clipping, seed, initial scalar state, input elements, and the
replicated world-size portability switch.

The dataset is bounded to 64 MiB and contains exactly `inputs` and `targets`: nonempty,
equal-shape, finite CPU float32 tensors. The engine cycles the sample axis deterministically and
uses stable rank-strided sharding. Progress means committed optimizer steps; checkpoint state also
records exact microbatch/sample counters and the global data cursor.

Before entering the device/distributed path, admission checks per-sample width times global
microbatch size against the model-owned input bound. It also caps a conservative 256 MiB peak. The
decode phase accounts for the encoded artifact, parser tensors, and owned clones; the encoded
bytes are released before device allocation. The runtime phase accounts for decoded plus device
copies of both dataset tensors, all accumulated input/target batches, four accumulated input-sized
activation/autograd buffers, and the three int64 arange/add/remainder index buffers. This global
pre-sharding accounting is intentional because the reference loader constructs a global batch
before rank sharding.

Required metadata binds the run and checkpoint resource IDs, config/model/code/toolchain/runtime
image/compatibility-policy digests, backend, device type, world size, local world size, and the
canonical topology digest. Optional metadata binds resume topology/identity, source revision,
classification, and cohort identity. Unknown metadata is rejected.

## Execution and outputs

The closed topology matrix is single-node Gloo/CPU worlds 1 and 2, plus NCCL/CUDA worlds 1 and 8.
Only local CPU and real two-process Gloo execution are evidenced in this source slice. CUDA/NCCL
admission still fails when the requested devices are unavailable and does not constitute H100
qualification.

Training composes the model-owned affine implementation, the authoritative eager float32
`Trainer`, AdamW factory, exact global-denominator reduction, DDP wrapper/reducer, and distributed
checkpoint adapter. Resume requires fresh model and optimizer objects and an identity-matching
committed checkpoint. Same-world resume is exact; the underlying adapter limits explicit
replicated portability to its documented 1-to-2 or 2-to-1 matrix. The worker rejects a restored
cursor that differs from the global sample counter. Run evidence records exactness, source rank,
source world size, and target world size; cross-world portability is explicitly non-exact and
cannot satisfy an exact checkpoint-resume qualification claim.

Rank zero alone returns this stable, atomically attested result order:

1. the canonical distributed-checkpoint manifest reference;
2. the immutable checkpoint-commit reference;
3. the eager safetensors model bundle;
4. canonical run evidence binding inputs, outputs, counters, topology, loss, attempt, and fence.

Bundle and evidence objects are staged before the terminal checkpoint transaction. Clock,
cancellation, and deadline observations are nonthrowing and synchronized after each optimizer
step before rank-zero telemetry, immediately after DCP save, and after rank-zero staging. The last
of these is the final collective. Rank zero may commit only after that reduction; there are no
collectives or executor deadline reclassification after the irreversible boundary. Nonzero ranks
return no output references, and the supervisor must treat the complete torchrun world (including
rank zero) as the execution unit. An active MLflow exporter is an optional mirror only: success
maps to `FINISHED`, cancellation/deadline to `KILLED`, and other faults to `FAILED`; mirror outages
never override CAS/checkpoint authority. Required-mode MLflow exporters are rejected at worker
construction because a mirror cannot participate in the atomic terminal transaction.

Attempt-scoped workspaces are retained on execution failure for bounded diagnostic/reaper handling.
After all outputs are terminally committed, cleanup is BaseException-contained and best effort;
the bounded scratch reaper owns any retained workspace. Cleanup cannot mutate the already-built
result, reclassify the committed status, or trigger a duplicate retry.

## Source qualification entry point

`//services/workers/training:training_qualification` runs a provider-free CPU source check and
emits exactly one JSON line on success. Its schema is
`mindclade.dev/reference-training-local/v1` and explicitly sets
`connected_qualification=false`.

The checked-in held templates refer to `/opt/mindclade/bin/training-qualification` under
`torchrun`, but they cannot produce connected evidence yet. No connected implementation of the
canonical checkpoint committer exists in this slice. The checkpoint-agent socket protocol,
qualified immutable runtime image, H100 hardware lane, and connected evidence collector are also
absent. The source CLI therefore rejects H100 phases and any checkpoint-socket argument with exit
code 2.

## Explicit nonclaims

This slice does not claim scientific model semantics, a general data pipeline, FSDP, multi-node
training, arbitrary resharding, mixed precision, ONNX/AOT export, CUDA or TileLang kernels, H100
numerical/performance qualification, MLflow control-plane authority, a deployable image, GKE
activation, artifact publication from this repository session, or production readiness of the
`models/` and `training/` umbrellas.
