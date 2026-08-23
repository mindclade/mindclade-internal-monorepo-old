# Runbook: node agent

Serves the `runtime.node_agent` component (`services/node_agent`).

## Scope note, stated plainly

The binary composes and stays resident. `services/node_agent/src/bootstrap.rs` reads its
configuration from the environment, constructs the artifact object store, registers both components
with the `servicekit` lifecycle, and serves a bounded operational plane. Probes have something to
target:

| Endpoint | Meaning |
| --- | --- |
| `GET /healthz` | Liveness. 503 once the accounting latch trips — see the hazard below. |
| `GET /readyz` | Readiness. Re-probes the artifact object store on every request. |
| `GET /metrics` | The three stage counters plus the health gauges. |

What it does **not** serve is stage traffic. The ticketed stage path has no wire contract in
`protocols/`, so there is no port to send a stage to; work still arrives only through in-process
calls to `mindclade_node_agent`. Readiness is scoped to what the operational plane actually answers
for, and a ready node is not by itself evidence that anything can dispatch stages to it.

The process fails closed. A missing or unparsable environment variable, an artifact root that is
not an absolute path, or an object store that does not answer within five seconds at startup all
exit **78** (`EX_CONFIG`) with the fault message on stderr, before the listener binds.

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

1. Read the health snapshot first — `GET /healthz` renders it, and `accounting_healthy == false`
   is the diagnosis; nothing else will restore admission. The latch is published as `Unhealthy`,
   so `/healthz` answers 503 and an orchestrator with a liveness probe restarts the node without
   being asked. That is the intended recovery, not a symptom to suppress: capture the body before
   the restart lands, because it is the only record of the latch firing.
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

- No stage wire protocol. The process listens only for the probes and metrics above; stages cannot
  be dispatched to it over the network because `protocols/` defines no contract for them.
- Exactly three counters exist — `node_agent.stage_started`, `stage_completed`, `stage_failed`
  (`services/node_agent/src/telemetry.rs:29-31`). They are now exported at `GET /metrics`
  alongside `node_agent_active_stages`, `node_agent_accepting`, and
  `node_agent_accounting_healthy`. The exposition is plain `name value` text, not a registered
  metrics format with types or labels.
- Those counters describe stage outcomes only. There is no admission-decision counter, no latency
  measurement, and no rejection-reason breakdown. Questions about *why* work was refused cannot be
  answered from this service's own telemetry.
- No logs and no traces.
