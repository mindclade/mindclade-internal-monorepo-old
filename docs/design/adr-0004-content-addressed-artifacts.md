# Use content-addressed immutable platform artifacts

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Mindclade internal monorepo

## Context

Datasets, reference databases, preprocessing bundles, checkpoints, model bundles, evaluation evidence, and releases must be reproducible and independently verifiable across services and clusters.

## Decision

Artifact bytes are addressed by digest and published atomically through manifests. The platform artifact CAS is separate from the Bazel action cache and Nix binary cache. Go owns catalog/policy metadata; Rust owns high-throughput byte transfer and verification; Python owns scientific/checkpoint semantics.

## Consequences

- Mutable aliases resolve to immutable digests.
- Garbage collection is reachability/retention policy, never path deletion by convention.
- Every artifact records provenance and schema compatibility.

## Enforcement

- Runtime and release paths verify digests before acceptance.
- Corruption and manifest mismatch are fail-closed incidents.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does not change the accepted architecture.
