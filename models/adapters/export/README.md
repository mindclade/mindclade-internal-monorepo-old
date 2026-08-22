# Models / Adapters / Export

**Status:** bounded `torch.export` local artifact slice implemented and locally
tested on CPU; ONNX and AOTInductor remain explicit scaffolds.

## Contract

`export_bundle` accepts an evaluation-mode `nn.Module`, a flat positional tuple
of tensors, and one immutable `TensorInputContract` per input. Each contract
records its positional name, exact example shape, dtype, device, contiguous
strided layout, gradient flag, and any named dynamic axes. Non-contiguous views
are rejected rather than relying on undocumented exported stride behavior.
Dynamic axes have explicit inclusive
minimum and maximum bounds; unspecified axes remain static. Shared dynamic names
mean equal sizes and must have identical bounds and example values.

The adapter uses strict `torch.export.export` capture and publishes a directory
containing exactly:

- `program.pt2`, written with `torch.export.save`;
- `manifest.json`, canonical schema-v1 JSON.

The frozen manifest records the artifact byte size and SHA-256 plus caller-owned
SHA-256 identities for resolved configuration, source, runtime, and kernel
manifest. It also records the PyTorch version and complete input contract. These
identities use the canonical `sha256:<64 lowercase hex>` spelling.

## Atomicity and load safety

Saving creates a private sibling directory, flushes both files, atomically
reserves a new destination, links the artifact, and publishes the manifest last
as the commit marker. It never overwrites an existing destination, including a
destination created concurrently. Publication is local-filesystem only; remote
object-store commit and catalog policy belong to their owning artifact services.

Loading is fail-closed. The caller must provide the expected manifest, active
runtime, and active kernel-manifest SHA-256 identities from trusted channels;
the recorded PyTorch version must exactly match the loader. The loader rejects symlinked bundles or files,
non-regular/extra/missing files, non-canonical or unknown manifest fields,
duplicate JSON keys, oversized files, size mismatches, and checksum mismatches.
It reads through no-follow descriptors and checks for changes during each read.

`torch.export.load` is pickle-capable in PyTorch. This adapter invokes it only
after the externally anchored manifest and artifact have both verified. A digest
received from the same untrusted source as the bundle is not a trust anchor.

## Validation and limits

`validate_export_parity` constructs a fresh eager module, requires every state
key, shape, dtype, device, and layout to match before restoration, strictly
restores a clone of the exported state, and compares eager and loaded-program
outputs across bounded input cases with finite explicit tolerances. Tests cover
minimum and maximum dynamic shapes on CPU, including the decoder-only LLM slice.
This package does not establish accelerator,
ONNX, AOTInductor, performance, registry, serving, or deployment qualification.
