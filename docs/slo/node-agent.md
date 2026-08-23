# Node agent SLO

**Status:** no approved objective. The shipped binary is a composition seam, not a server.

`services/node_agent` provides a reusable ticketed execution core, but its binary prints one line
stating that provider and tool composition are deployment-owned, and exits
(`services/node_agent/src/main.rs:7-11`). No process stays resident, so there is no admission stream
to measure and no target for a liveness probe. Objectives are defined before production promotion;
this component is not at that point.

## Unratified candidate — not an agreed target

A previous revision recorded `99.9%` availability "for admitted production traffic where
applicable" — the same sentence, byte for byte, as four other unrelated SLO documents, with no
owner, window, or measurement behind it. It is kept here as an **unratified candidate** so the
earlier choice is not silently erased, and it carries no authority as a commitment. Ratification
requires staging measurements from a resident agent and owner sign-off.

## Indicators that exist today

`NodeMetrics` maintains three counters — `node_agent.stage_started`, `stage_completed`,
`stage_failed` (`services/node_agent/src/telemetry.rs:15-23`). They are held in an in-process
`CounterRegistry` reachable only via `snapshot()`, with **no exporter**. A stage success ratio is
therefore derivable in principle but not observable in practice until deployment wiring exports the
registry. That export is a prerequisite for any SLI.

Note what these three counters cannot express: they count stage outcomes, not admission decisions,
not latency, and not rejection reasons. An availability objective phrased in terms of admitted
traffic has no supporting indicator here at all.

## Failure modes that must be reflected in any objective

- **Accounting corruption is a one-way latch.** `mark_accounting_corrupt` clears both
  `accounting_healthy` and `accepting` (`services/node_agent/src/health.rs:90-93`) and fires on
  counter overflow or underflow (`:44`, `:67`). Nothing sets either flag back to true, so recovery
  is **restart-only**. On a node agent this means the node stops accepting work until it is
  replaced; any objective must budget for that, and any alert must distinguish it from an ordinary
  drain, because `accepting` reads false in both cases.
- **Admission and health share one flag.** The admission check tests `accepting` and
  `accounting_healthy` together (`services/node_agent/src/health.rs:36-38`), so a corrupted counter
  and a deliberate drain are indistinguishable from the outside without the
  `accounting_healthy` field in the snapshot (`:87`).

## Correctness invariants (release-blocking, not traded for availability)

Work is never admitted outside valid ticket authority, and resource accounting is never resumed by
clearing the corruption latch by hand. A node whose accounting is corrupt is fenced, not coaxed
back. Bounded admission, cancellation, and shutdown budgets must be release-qualified before
production promotion; they are not release-qualified today. SLO exclusions require an incident or
evidence record, not an ad hoc dashboard annotation.
