# Tools / Qualification / Build

- **Status:** Remote-execution source qualification implemented; connected deployment evidence pending.
- **Owner:** `@mindclade/platform`.

## Purpose

Repository-owned code generation, analysis, developer, qualification, and release tools. Tools are invoked through Bazel targets in production/CI paths. This path specializes that domain for **build**.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

## Implemented contract

`verify_execution_image.py` validates the Buildfarm 2.17.0 source/image lock and requires
native AMD64/ARM64 Nix image attestations with two identical independent rebuilds per
platform. `check_remote_execution.py` compares local and connected Buildfarm result evidence,
including output digests, Bazel/toolchain/image identity, and proof that at least one action
executed remotely rather than merely hitting a cache.

Both validators fail if network access or host-path inputs are reported. The parity record is
an evidence validator, not an evidence collector: the protected native qualification workflow
must produce the input JSON from Bazel execution logs and uploaded output manifests.

No source-only invocation proves private connectivity, worker cancellation, cache eviction,
multi-zone availability, or SLOs. Production clients must retain local execution as rollback
until the exact endpoint and image digests pass the connected gates.
