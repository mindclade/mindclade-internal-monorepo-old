# TypeScript API client state

- **Status:** Implemented and unit tested; environment integration remains unqualified.
- **Primary implementation ownership:** the language indicated by the second path segment

## Purpose

Observable resource state, normalized request-error classification, event
channels, and bounded async pagination for browser consumers. Canonical wire
types and HTTP operations remain owned by `sdk/typescript`; this package adds
UI-facing lifecycle state without duplicating the generated contract.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

Consumers supply the fetch operation and cancellation policy. Resource stores
are in-memory and per-process; they do not persist credentials or payloads.
Breaking state or error-shape changes require coordinated application updates.
