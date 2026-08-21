# TypeScript molecular viewer

- **Status:** Implemented and unit tested; scientific and large-structure qualification remains open.
- **Primary implementation ownership:** the language indicated by the second path segment

## Purpose

Bounded PDB text loading and parsing, atom selection, and deterministic,
accessible SVG projection for lightweight result inspection. Network requests
accept cancellation, enforce response-size limits, and surface parse failures.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

This is not a molecular-dynamics engine, validation authority, or replacement
for specialist 3D tooling. Consumers must treat the rendering as inspection
only and preserve source artifacts for scientific evidence.
