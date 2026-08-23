# ADR-0023: Persist resolved configurations and enforce dependency budgets

- **Status:** Accepted
- **Date:** 2026-08-13
- **Superseded in part by:** [ADR-0024](adr-0024-dependency-layering-over-counts.md) — the dependency-budget half only

## Decision

Composable profiles resolve to one canonical configuration document/digest.
Runs, checkpoints and releases reference that resolved digest. Selected
implemented components additionally have explicit direct-dependency budgets and
allow/deny prefixes enforced in presubmit.

Configuration composition and dependency enforcement are mechanisms; semantic
model/training/serving policy remains with owning domains.

> **Partly superseded, 2026-08-19.** The resolved-configuration half of this
> decision stands. The dependency-budget half does not: ADR-0024 replaced
> counting direct dependencies with prefix-based layering, and
> `tools/analysis/check_dependency_budgets.py` now rejects the
> `max_internal_direct` key outright. Budgets are still enforced in presubmit,
> but by allow/deny prefix rather than by count.
