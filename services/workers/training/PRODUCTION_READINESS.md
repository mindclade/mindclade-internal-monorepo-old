# Training worker production readiness

**Current status:** bounded reference-affine implementation; deployment remains disabled.

- [x] Unified training stage kind, deadline, cancellation, concurrency, and drain behavior are
  locally tested.
- [x] `reference.affine.train.v1` has strict canonical config/dataset/identity parsing, bounded
  eager training, checkpoint/resume, export, evidence, and rank-zero publication behavior.
- [x] World-size-one CPU and a real two-process Gloo/DDP/DCP service path pass focused Bazel tests.
- [x] A worker-produced eager bundle has exact CPU parity with the reference serving runtime.
- [x] Optional-only MLflow mirroring is failure-contained and maps terminal stage classifications;
  required-mode exporters are rejected before execution.
- [x] The canonical manifest builder, injected terminal-committer seam, post-commit cleanup
  semantics, and unsupported workload/topology claims are documented and locally tested.
- [ ] Rust-to-Python process and bulk-buffer integration plus fault injection pass.
- [ ] A connected Rust/artifact-plane and Go-registry implementation of `CheckpointCommitter`
  verifies components and atomically commits manifest-last checkpoint, registry record, exact
  outputs, stage status, fence, and deadline.
- [ ] The checkpoint-agent socket protocol and immutable qualification image exist and are wired.
- [ ] Atomic output/status commit rejects stale attempts in an end-to-end supervisor test.
- [ ] H100 one-GPU and eight-GPU NCCL/DCP correctness, determinism, interruption, and performance
  evidence pass the held connected lane.
- [ ] Resource limits, production failure diagnostics, and orphan/reaper operations are qualified.
- [ ] Image, SBOM, provenance, security, rollback, alerting, and runbook evidence pass.

The local `training_qualification` result is explicitly non-connected and cannot satisfy any
unchecked item. Kubernetes activation must remain suspended until every unchecked item links
concrete connected evidence.
