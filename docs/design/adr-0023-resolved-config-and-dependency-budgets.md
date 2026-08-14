# ADR-0023: Persist resolved configurations and enforce dependency budgets

- **Status:** Accepted
- **Date:** 2026-08-13

## Decision

Composable profiles resolve to one canonical configuration document/digest.
Runs, checkpoints and releases reference that resolved digest. Selected
implemented components additionally have explicit direct-dependency budgets and
allow/deny prefixes enforced in presubmit.

Configuration composition and dependency enforcement are mechanisms; semantic
model/training/serving policy remains with owning domains.
