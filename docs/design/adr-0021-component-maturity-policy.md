# ADR-0021: Make scaffold maturity machine-readable

- **Status:** Accepted
- **Date:** 2026-08-13

## Decision

`components.toml` is the selected-component status inventory and
`maturity.toml` defines advancement evidence. Files/directories do not imply
implementation. Scaffolded/experimental components cannot become production
dependencies by path existence alone.

Presubmit validates owner, tests, qualification, SLO, runbook and release-target
requirements appropriate to each maturity state.
