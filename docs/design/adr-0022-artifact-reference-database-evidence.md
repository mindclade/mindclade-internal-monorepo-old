# ADR-0022: Bind artifacts, reference data, and release evidence by immutable identity

- **Status:** Accepted
- **Date:** 2026-08-13

## Decision

Artifact identity is digest/size/media-type/logical-kind/schema-version and is
separate from storage location. Reference databases are promoted immutable
releases composed of artifact identities and provenance. Model/runtime release
qualification is an evidence DAG whose nodes are immutable evidence artifacts.

Object-store paths, mutable database directories and CI job success are not
resource identities or sufficient release evidence.
