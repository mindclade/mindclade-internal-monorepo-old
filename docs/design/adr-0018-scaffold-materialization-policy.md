# ADR-0018: Evidence-based scaffold materialization

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Repository target tree and completion claims

## Context

A comprehensive target-state blueprint can create thousands of paths and the
appearance of implementation. Empty symmetry increases maintenance and makes it
hard to distinguish architecture intent from working production capability.

## Decision

The repository materializes every approved blueprint path so ownership and
future dependency direction are explicit. Nonimplemented files clearly declare
**target-state scaffold** status and never contain fake success behavior.

A capability becomes implemented only with a named owner, stable contract,
source, tests, Bazel target, docs, explicit limits/failure behavior, and required
qualification. A deployable becomes production-ready only when its local
`PRODUCTION_READINESS.md` links actual evidence.

## Consequences

The tree is complete for navigation without making false implementation claims.
Work proceeds by complete vertical slices rather than empty horizontal package
creation. Status and evidence override path existence.

## Enforcement

Repository checks reject generic placeholder markers in released docs, empty
production factories, unsafe fallback to memory providers, missing ownership or
BUILD targets on promoted packages, and unsupported completion claims.
