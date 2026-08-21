# TypeScript operational charts

- **Status:** Implemented and unit tested; visual-regression qualification remains open.
- **Primary implementation ownership:** the language indicated by the second path segment

## Purpose

Dependency-free React SVG line, histogram, heatmap, and topology views for
bounded operational datasets. Every chart includes a textual summary and does
not rely on color alone to communicate status.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

Inputs must be pre-aggregated; this package is not a streaming store or a
general plotting engine. Empty and non-finite values are handled explicitly,
and consumers own domain-specific scales and retention.
