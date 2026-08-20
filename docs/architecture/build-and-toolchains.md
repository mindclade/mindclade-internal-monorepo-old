# Build and toolchains

## Decision

Bazel with Bzlmod owns the build graph. Nix owns pinned toolchains and execution
environments. They do not compete for dependency or release authority.

## Bazel owns

- build analysis and compilation;
- tests, code generation, linting, and affected-target selection;
- OCI application images and release bundles;
- numerical, performance, safety, security, and scale qualification;
- SBOM, provenance, signatures, and promotion targets;
- local/remote execution platform selection.

## Nix owns

- Bazel, Go, Rust, Python, Node, C/C++, CUDA/ROCm, Protobuf, and docs tools;
- developer and CI shells;
- normalized toolchain bundles and manifests;
- system/runtime closures and remote-execution worker bases;
- trusted binary caches.

```text
flake.nix + flake.lock
  -> Nix toolchain closure + manifest
  -> Bzlmod extension and registered toolchains
  -> Bazel build/test/package/image/qualify/release

same Nix derivation
  -> immutable remote worker/base image
  -> digest verified against the manifest
```

## Rules

- Bzlmod only; no legacy dependency graph;
- one root module/workspace per language unless independently published;
- no host tool or package installation leakage;
- the same Nix derivations back local and remote toolchains;
- Nix cache, Bazel CAS, and platform artifact CAS remain separate;
- production OCI images are Bazel outputs built from Nix-produced bases;
- generated compatibility version files are verified, not independently owned.

## Developer path

```bash
tools/dev/nixw develop .#default
bazel test //...

tools/dev/nixw develop .#gpu --command bazel test --config=cuda //tests/...
```

This scaffold includes target files and checks; connected CI must populate and
verify real locks/toolchains before a full build claim.

## Go module checksum closure

Internal Go code has one root `go.mod` and one root `go.sum`. The committed
`go.sum` must authenticate every direct public requirement and its `go.mod`.
The connected Go qualification lane then executes:

```text
go mod download all
go mod verify
go mod tidy -diff
```

This deliberately separates **committed dependency identity/checksums** from
**network/provider qualification**. A sandbox that cannot reach the production
module mirror may still validate the direct checksum seed and all offline-safe
packages, but only the connected pinned environment can certify complete
transitive source availability and a tidy module graph.
