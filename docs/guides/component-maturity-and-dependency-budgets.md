# Component maturity and dependency budgets

The complete scaffold is a navigable target-state map, not a claim that every
file is production code. `components.toml` gives selected components one of:

`planned`, `scaffolded`, `experimental`, `implemented`, `qualified`,
`production`, or `deprecated`.

`maturity.toml` defines evidence required to advance. Implemented code requires
tests. Qualified code additionally requires qualification evidence. Production
requires qualification plus SLO, runbook and release target. Scaffolded or
experimental components cannot be used as production dependencies merely
because their path exists.

`architecture/dependency_budgets.toml` is intentionally selective. A budget is
added when a component has a real architecture worth protecting, not for every
placeholder directory. The presubmit checker ignores Rust dev-dependencies when
measuring production direct edges and enforces Go/Rust allow/deny prefixes.
