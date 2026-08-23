# Runbook: node agent

Serves the `runtime.node_agent` component (`services/node_agent`).

## Scope note, stated plainly

The shipped binary is a composition seam. It prints one line saying provider and tool composition
are deployment-owned, then exits (`services/node_agent/src/main.rs:7-11`). No agent process stays
resident on any node today, and a liveness probe has nothing to target. What follows documents the
reusable ticketed core in `mindclade_node_agent` so that it is correct when deployment wiring
exists.

## Trigger

A node stops accepting stage work, the stage-failure counter climbs, or work is rejected while the
node appears otherwise healthy.

## Hazard — the accounting latch is one-way, and on a node it fences the node

`mark_accounting_corrupt` clears **both** `accounting_healthy` and `accepting`
(`services/node_agent/src/health.rs:90-93`), and fires on counter overflow or underflow
(`:44`, `:67`). Nothing sets either flag back to true.

On a node agent this is more consequential than on a shared service: the node stops accepting work
permanently and does not recover on its own.

- **Recovery is restart or node replacement.** There is no reset path, and no amount of waiting
  helps.
- The admission check reads `accepting` and `accounting_healthy` together
  (`services/node_agent/src/health.rs:36-38`), so a fenced node and a draining node are
  indistinguishable from `accepting` alone. Read `accounting_healthy` from the snapshot (`:87`)
  before concluding a node is merely draining.
- Overflow or underflow means resource accounting contradicted itself. A restart clears the latch
  but not the cause. Capture the health snapshot and the three stage counters before restarting, or
  the only evidence is lost.

Do not introduce a setter to clear this latch operationally. Accounting that has proven inconsistent
must not keep admitting work onto a node — the latch is the fence.

Escalate to node replacement when accounting corruption recurs on the same node, per
`runtime-host-degraded.md` step 6.

## Triage

1. Read the health snapshot first. `accounting_healthy == false` is the diagnosis; nothing else
   will restore admission.
2. Otherwise, distinguish a deliberate drain (`drain` clears `accepting` only,
   `services/node_agent/src/health.rs:80`) from a fence. Only the fence clears
   `accounting_healthy`.
3. Compare `node_agent.stage_started` against `stage_completed` plus `stage_failed`
   (`services/node_agent/src/telemetry.rs:15-23`) to see whether stages are being lost rather than
   failing. These three counters are the entire signal surface — see the limitations below.

## Recovery

- Fenced node: capture snapshot and counters, then restart the agent or replace the node.
- Drain: allow in-flight stages to complete; do not force-clear `accepting`.
- Ticket or authority failures: reject rather than bypass verification, and follow
  `ticket-key-rotation.md` when the cause is rotation.
- GPU or model-slot involvement: confirm worker termination before releasing reservations, per
  `runtime-host-degraded.md` step 5.

## Exit criteria

The node reports `accounting_healthy`, admission is restored through a clean lifecycle rather than a
manual flag change, in-flight stages were not silently dropped, and no work was admitted outside
valid ticket authority.

## Known limitations recorded here deliberately

- Binary is a stub; no agent runs today.
- Exactly three counters exist — `node_agent.stage_started`, `stage_completed`, `stage_failed`
  (`services/node_agent/src/telemetry.rs:15-23`) — held in an in-process registry with **no
  exporter**. They cannot be scraped until deployment wiring exports the registry.
- Those counters describe stage outcomes only. There is no admission-decision counter, no latency
  measurement, and no rejection-reason breakdown. Questions about *why* work was refused cannot be
  answered from this service's own telemetry.
- No logs and no traces.
