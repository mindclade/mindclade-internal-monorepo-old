# Bazel with Bzlmod is the only build graph

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Mindclade internal monorepo

## Context

The monorepo spans Go, Rust, Python, TileLang, TypeScript, Protobuf, OCI images, infrastructure validation, qualification, and release evidence. Multiple independent build graphs would make affected analysis, provenance, remote execution, and compatibility enforcement inconsistent.

## Decision

Bazel owns build analysis, code generation, compilation, tests, packaging, OCI images, qualification, release bundles, SBOM attachment, and provenance. Dependencies are declared through Bzlmod (`MODULE.bazel` and its lock); legacy `WORKSPACE` dependency resolution is not used.

## Consequences

- One affected-target graph covers every language and release artifact.
- Rules and external dependencies must be pinned and qualified before use.
- Host-only shortcuts are rejected even when they improve local convenience.

## Enforcement

- CI runs Bzlmod lock and hermeticity checks.
- Production artifacts are emitted by Bazel targets, not ad hoc scripts or Docker builds.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does not change the accepted architecture.
