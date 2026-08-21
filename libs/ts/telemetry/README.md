# TypeScript telemetry

- **Status:** Implemented and unit tested; collector integration remains unqualified.
- **Primary implementation ownership:** the language indicated by the second path segment

## Purpose

A bounded browser event queue with sensitive-field redaction, explicit flush,
beacon support during page teardown, and fetch fallback. When no endpoint is
configured, collection is disabled and no events leave the process.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

Telemetry is best-effort and never part of a transaction or audit trail. Events
must remain metadata-only: model payloads, credentials, prompts, molecular
content, and unrestricted free text are outside the accepted contract.
