# ADR-0026: Orchestration and scheduling are separate owners over one shared work queue

- **Status:** Accepted
- **Date:** 2026-08-23
- **Scope:** `control/orchestration`, `control/scheduling`, and their boundary with `control/runs`

## Context

`control/orchestration` and `control/scheduling` were promoted to production-grade
implementations at the same time, and the original blueprint left three boundaries ambiguous.
Attempt state could plausibly be modelled either as a new Go vocabulary or as a mirror of the
existing worker state machine. Run-level identity and cancellation already had an owner in
`control/runs`, but stage and attempt lifecycle did not. Most damagingly, the blueprint listed
Kueue and JobSet adapters under both packages, which would give two writers the same Kubernetes
objects — the exact shape of the split-brain defect that fencing exists to prevent everywhere
else in the control plane.

Deciding these three boundaries once is cheaper than discovering them through divergent state
vocabularies and conflicting object writes in production.

## Decision

**Attempt state vocabulary.** Go mirrors the Rust transition table in
`libs/rust/worker_runtime/src/machine.rs` and the `WorkerState` enum in
`protocols/proto/mindclade/runtime/v1/worker_status.proto` rather than inventing a second
vocabulary for the same lifecycle. Stage state is a separate vocabulary derived from `RunState`,
adding exactly one state: `blocked`. The addition is principled rather than convenient — a run
has nothing upstream of it, whereas a stage can be waiting on a dependency, and that distinction
has no representation in the run vocabulary.

**orchestration ↔ runs.** `control/runs` owns run and job identity, the client-visible
`RunState`, and cancellation *intent*. `control/orchestration` owns workflow, stage, and attempt
state, cancellation *propagation*, leases, and the executor. Orchestration treats RunID and JobID
as opaque canonical identifiers; it does not reinterpret, re-derive, or re-issue them.

**orchestration ↔ scheduling.** The two packages communicate only through the
`control-plane/placement` durable work queue. Orchestration compiles the plan and owns
stage/attempt state; scheduling decides whether, where, and when a workload runs. The
orchestration Kubernetes adapter owns the JobSet object lifecycle for a launched attempt; the
scheduling Kueue adapter owns queue and capacity projection. One writer per object, with no
shared adapter ownership.

## Consequences

- Worker lifecycle has one vocabulary across Rust, the protocol, and Go; a state added in one
  place is a protocol change rather than a local Go enum drift.
- Stage state remains legible to anyone who already knows `RunState`, with a single documented
  addition to learn.
- Cancellation has one authoritative intent holder and one propagator, so a cancelled run cannot
  be resurrected by a stage-level decision.
- Orchestration and scheduling can be released, stalled, and recovered independently, because
  their only coupling is a durable queue rather than a synchronous call or a shared object.
- The blueprint's duplicate Kueue/JobSet adapter listing is resolved: each Kubernetes object has
  exactly one writer, and neither package may reach into the other's adapter.

## Enforcement

- `control/orchestration` and `control/scheduling` expose no direct dependency on each other;
  the placement queue name is the sole shared contract.
- Stage and attempt vocabularies are covered by the state-machine and attempt tests declared for
  both components in `components.toml`.
- Adapter ownership is enforced by package boundaries: the JobSet adapter lives under
  orchestration and the Kueue adapter under scheduling, and neither is re-exported.

## Supersession

A later ADR must explicitly supersede this decision; implementation drift does not change the
accepted architecture.
