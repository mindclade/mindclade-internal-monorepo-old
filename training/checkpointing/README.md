# Training checkpointing

This package implements one deliberately bounded checkpoint contract: exact local resume for a
single-process, CPU, float32 PyTorch training run. A commit contains model state, optimizer state,
the CPU Torch RNG, authoritative `TrainingState` counters, a caller-owned data position, and the
config/data/model/code/toolchain/topology identities that must match before restore.

The state format is pickle-free. Tensor payloads use safetensors; the typed state tree and manifest
use canonical, unique-key JSON. Each artifact is content-addressed with the repository's canonical
`ArtifactRef`. Publication writes and fsyncs a sibling staging directory, writes the manifest last,
and atomically renames the complete directory. Restore accepts only the exact committed member set,
rejects symlinks and changed files, verifies size and digest before mutation, and requires fresh model
and optimizer objects because PyTorch's load APIs are not transactional.

## Contract and limits

- World size is exactly one; floating-point model parameters and buffers must be finite CPU
  float32 tensors.
- The optimizer must own every trainable model parameter exactly once.
- Tensor state is bounded to 256 MiB and the committed directory to 512 MiB.
- Only dense, non-complex, non-quantized tensors and bounded JSON-safe optimizer state are accepted.
- A checkpoint destination is immutable. Retry with a new checkpoint ID and directory.
- Data-loader/sampler state remains caller-owned and is represented by the validated data position.

Focused validation is owned by `//training/checkpointing/tests:test_resume`.

This is an implemented reference library, not a production-readiness claim for the `training`
umbrella. Distributed Checkpoint, resharding, remote object-store publication, retention, schema
migrations, GPU RNG, protocol wire manifests, and service composition remain scaffolds. Those paths
must begin with their canonical protocol and topology contracts and need connected fault-injection,
cross-topology, security, and operational qualification before promotion.
