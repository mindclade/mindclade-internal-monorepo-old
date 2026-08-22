# Training checkpointing

This package implements two deliberately bounded checkpoint contracts. `local-v1` provides exact
local resume for a scheduler-free, single-process CPU float32 PyTorch run. A commit contains model
state, optimizer state, the CPU Torch RNG, authoritative `TrainingState` counters, a caller-owned
data position, and the config/data/model/code/toolchain/topology identities that must match before
restore. Scheduler state is not part of either format. Trainer owners must use
`save_local_trainer_checkpoint` or `save_distributed_trainer_checkpoint`; both reject a Trainer with
a scheduler before creating staging state. The component-level functions exist for owned
orchestration that never installs a scheduler and must not be used to claim scheduled exact resume.

The state format is pickle-free. Tensor payloads use safetensors; the typed state tree and manifest
use canonical, unique-key JSON. Each artifact is content-addressed with the repository's canonical
`ArtifactRef`. Publication writes and fsyncs a sibling staging directory, writes the manifest last,
and atomically renames the complete directory. Artifact references are derived from prepared bytes,
not reread staging contents; publication reads back exact expected bytes before writing the manifest,
and distributed publication semantically decodes the committed components before reporting success.
Restore accepts only the exact committed member set, rejects symlinks and changed files, verifies size
and digest before mutation, and requires fresh model and optimizer objects because PyTorch's load APIs
are not transactional.

## Local-v1 contract and limits

- World size is exactly one; floating-point model parameters and buffers must be finite CPU
  float32 tensors.
- The optimizer must own every trainable model parameter exactly once.
- AdamW state, when present, must cover every parameter; every scalar step must be a finite bounded
  integer, identical across parameters, and equal to `TrainingState.optimizer_steps`. Save checks
  live state and restore checks decoded state before either PyTorch load API can mutate objects.
- Tensor state is bounded to 256 MiB and the committed directory to 512 MiB.
- Only dense, non-complex, non-quantized tensors and bounded JSON-safe optimizer state are accepted.
- Metadata is rejected before parsing when JSON syntax exceeds 256 nested objects/arrays, so the
  admission boundary is identical across supported Python runtimes and ignores bracket bytes inside
  quoted strings.
- A checkpoint destination is immutable. Retry with a new checkpoint ID and directory.
- Data-loader/sampler state remains caller-owned and is represented by the validated data position.

`distributed-v1` adds replicated DDP state for CPU/Gloo and CUDA/NCCL float32 runs. It obtains
canonical model and optimizer FQNs with PyTorch's distributed state-dict APIs, stores one verified
replicated model/optimizer component, and stores CPU plus optional CUDA RNG state per rank. All
tensor payloads remain safetensors and all metadata remains canonical JSON; PyTorch DCP's
pickle-backed `.metadata` file is not emitted or accepted. Every rank participates in staging,
replica-equality checks, and manifest-last publication.

Same-world restore reinstates rank-local RNG and supports exact continuation within the pinned
runtime. Explicit world-size portability restores replicated model/AdamW state, counters, and the
global data position; it deliberately leaves RNG caller-seeded and reports `exact_resume=False`.
This is not FSDP resharding. Distributed-v1 accepts worlds 1, 2, and 8 and is bounded to
2 GiB in this reference adapter, requires initialized AdamW tensor state with
foreach/fused/capturable/differentiable execution disabled, and requires fresh restore objects. Its
AdamW step tensors obey the same exact counter binding as local-v1.

The accepted runtime matrix is CPU world 1/2 and CUDA world 1/8. World 2 uses same-node Gloo and
world 8 uses same-node NCCL through the distributed runtime. Replicated world-size portability is
strictly CPU/Gloo 1↔2; it rejects either a CUDA source or CUDA target even when the caller opts in.
CUDA world 1/8 checkpoints support same-world exact restore only.

## Storage trust and admission

The local-v1 and distributed-v1 directories are internal storage formats for trusted,
permission-isolated filesystems. Member digests detect corruption and inconsistent partial edits;
they are not authenticity proofs against an actor who can rewrite the whole tree and recompute both
artifact references and the manifest. Do not restore directly from an attacker-writable directory.
At an admission boundary, obtain the manifest digest from an independently trusted registry record
or committed checkpoint record and pass it as `expected_manifest_digest`. The restore APIs compare
that external `Digest` before decoding state or mutating model, optimizer, or RNG. If the external
commit is the authority, its own signature/admission policy remains the caller's responsibility.

Distributed cleanup is best effort. A failed staging cleanup is synchronized across ranks and fails
the operation without stranding peers in a post-cleanup barrier; operators may need to remove the
unpublished staging directory after resolving the filesystem fault.

Focused validation is owned by `//training/checkpointing/tests:test_resume` and
`//training/checkpointing/tests:test_dcp`.

This is an implemented reference library, not a production-readiness claim for the `training`
umbrella. Sharded DCP/resharding, remote object-store publication, retention, schema migrations,
protocol wire commit publication, and service composition remain separate or scaffolded. Those paths
must begin with their canonical protocol and topology contracts and need connected fault-injection,
cross-topology, security, and operational qualification before promotion.
