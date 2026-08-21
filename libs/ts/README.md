# TypeScript libraries

- **Status:** Implemented; application integration remains environment-qualified.
- **Primary implementation ownership:** the language indicated by the second path segment

Reusable browser mechanisms with one-way dependencies toward the generated SDK:

- `api_client`: observable resource state, error classification, and bounded pagination;
- `auth`: in-memory session state, cookie-backed auth endpoints, and scope guards;
- `browser_security`: CSP, response headers, and production endpoint validation;
- `telemetry`: bounded batching and fail-closed sensitive-field redaction;
- `design_system`: semantic tokens and accessible operational UI primitives;
- `charts`: dependency-free accessible SVG line, histogram, heatmap, and topology views;
- `molecular_viewer`: bounded PDB parsing, selection, loading, and deterministic projection.

## Boundary

Reusable implementation belongs in this owning package. Deployable entry points,
provider construction, health/drain wiring, and deployment evidence belong under
`services/`. Cross-language data exchanged outside a process uses versioned
contracts under `protocols/` rather than language-private structures.

This package must not become a `common`, `shared`, `helpers`, or `utils` dumping
ground. It may depend only in the direction documented by
`docs/architecture/dependency-rules.md` and the accepted ADRs.

Every package has a native TS7 build/typecheck, a package-local unit suite, and
a Bazel `ts_project`. Libraries do not own React application routing, backend
policy, secrets, or service composition. Breaking exports require a coordinated
SDK/app migration; rollback restores the previous workspace package commit.
