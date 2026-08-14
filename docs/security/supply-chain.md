# Software, model, and data supply-chain policy

## Build and toolchains

- Bazel is the sole build/test/codegen/image/qualification/release graph.
- Nix pins compilers, interpreters, SDKs, system libraries, developer shells,
  toolchain bundles, and remote-execution images.
- Bzlmod lockfiles own Bazel dependency resolution; host-tool and undeclared
  network/package installation are rejected.
- The local and remote execution-platform manifest digests must match.

## Release evidence

Every promoted service/runtime/model/data release includes as applicable:

```text
source revision and clean-tree state
Bazel invocation and target graph identity
Nix/toolchain and execution-platform digests
dependency locks and SBOM
image/bundle/artifact digests and signatures
build provenance/attestation
numerical, safety, performance, resilience, and security results
model/data/reference lineage and licenses
rollback target and revocation policy
```

## Dependency and secret controls

Dependencies are pinned, reviewed, vulnerability-scanned, license-checked, and
updated through controlled automation. CI uses workload identity and scoped
short-lived credentials; secrets are not placed in source, build actions,
artifacts, logs, or caches.

## Artifact policy

Datasets, checkpoints, reference snapshots, model/runtime bundles, kernel
qualification manifests, and evidence are content-addressed and accepted only
after verification and atomic manifest publication.
