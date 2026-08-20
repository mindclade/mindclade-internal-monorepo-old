# ADR-0024: Enforce dependency layering by prefix, not by counting dependencies

- **Status:** Accepted
- **Date:** 2026-08-19
- **Supersedes:** ADR-0023, in part — the dependency-budget half only

## Decision

Component dependency rules are expressed as allow/deny prefixes and enforced in
presubmit. Direct-dependency COUNTS (`max_internal_direct`) are removed from
`architecture/dependency_budgets.toml` and from
`tools/analysis/check_dependency_budgets.py`, which now rejects the key outright
rather than ignoring it.

Prefix rules extend to Rust and TypeScript alongside Go. The checker reads Go
imports, Rust `[dependencies]` path entries, and TypeScript workspace
dependencies from `package.json`. A budget entry declaring neither
`allowed_prefixes` nor `forbidden_prefixes` is an error, because a budget that
enforces nothing reads in review as though the component is governed.

ADR-0023's other decision — that composable profiles resolve to one canonical
configuration document/digest, and that runs, checkpoints and releases reference
that resolved digest — is unaffected and remains in force.

## Rationale

A count measures how much of a component exists. It does not measure whether the
component's layering holds, and the two move independently.

The evidence is in this repository's own history. `services/control_plane` went
40 → 44 → 62 → 68 direct internal imports in a single branch as the registry,
scheduler, controller and projector factories materialized. The ceiling was
breached three times and raised three times. Every raise was correct: a
composition root's job is to name every provider the fleet uses, so counting them
measures how much of the fleet has been built. Six roles remain unmaterialized,
so the number will move six more times and reject nothing on the way.

A rule that is right to relax every time it fires is not a rule. It is a speed
bump that teaches people to edit the config, and the habit it builds — reach for
the threshold first — is precisely the wrong reflex to have when a real layering
violation eventually fires.

The prefix rules have the opposite property. A Go service importing another Go
service, or a Rust crate reaching past its allowlist, is a defect at one import
or at fifty. Those rules do not want relaxing, and when one fires the correct
response is to change the code. That asymmetry — never relax versus always
relax — is the whole reason to keep one mechanism and drop the other.

Extending prefixes to Rust and TypeScript follows from the same argument. The
schema already carried a `language` field; Rust budgets existed but only Go had
been given deny rules, and TypeScript had no entries at all. A layering rule that
holds in one language and not the others is not an architectural constraint, it
is a Go convention.

## Consequences

Counts are gone, so a component that doubles its imports without crossing a
prefix boundary now passes. That is the intended trade: breadth is reported by
the maturity ladder (ADR-0021) and visible in review, whereas a threshold that
nobody trusts is worse than no threshold at all.

Four `services/control_plane/internal/*` budgets previously carried a count and
nothing else. They now carry the layering rule their comments already described
in prose — chiefly that provider construction belongs in `internal/providers` and
must not leak into bootstrap, config, foundation or transport.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does
not change the accepted architecture.
