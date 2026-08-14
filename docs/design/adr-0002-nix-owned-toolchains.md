# Nix owns toolchains and execution environments

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Mindclade internal monorepo

## Context

Hermetic Bazel actions still require a trusted, reproducible source of compilers, interpreters, SDKs, CUDA/ROCm closures, and remote-worker images.

## Decision

Nix flakes pin the toolchain closure and developer shells. The same derivations produce the normalized Bazel toolchain bundle and remote-execution base image. Bazel consumes those toolchains and owns all application outputs.

## Consequences

- Nix, Bazel, and platform artifacts use separate caches.
- Toolchain manifests and execution-image digests are release evidence.
- Flake evaluation must remain pure and version-pinned.

## Enforcement

- CI rejects host-tool leakage and toolchain-manifest drift.
- Compatibility version files are generated from the Nix-owned source.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does not change the accepted architecture.
