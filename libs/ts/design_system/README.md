# Mindclade design system

- **Status:** Implemented and unit tested; cross-browser accessibility qualification remains open.
- **Primary implementation ownership:** the language indicated by the second path segment

## Purpose

Semantic theme tokens and accessible operational primitives: buttons, metrics,
status indicators, and data tables. Components expose native semantics and keep
product-specific copy, policy, routing, and data acquisition in applications.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

The CSS token surface is the compatibility contract. Applications may compose
the primitives but should not fork their focus, motion, contrast, or status
semantics. Breaking token changes require a coordinated application migration.
