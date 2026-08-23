# ADR-0026: Keep workload identity and node placement as distinct types

- **Status:** Accepted
- **Date:** 2026-08-23

## Decision

`mindclade.orchestration.v1.WorkloadEnvelope` is one message with one meaning in
every language. A projection may narrow it -- drop or delegate a field the host
must not act on -- but may never contradict it: a field the projection keeps
carries the wire's name and the wire's concept.

Materialized bulk data is a separate concept and never travels inside the
envelope. The envelope names inputs and expected outputs by content identity
(`mindclade.common.v1.ArtifactRef`, ADR-0004); node-local placement travels
beside it as `mindclade.runtime.v1.BufferDescriptor`, exactly as
`RuntimeExecuteRequest` already models the node hop. The node binds the two by
refusing any materialized buffer whose content digest the envelope did not
authorize.

Each language's projection is declared, not inferred, and the declaration is
enforced by
`tests/integration/cross_language/test_workload_envelope.py`: the kept, renamed,
delegated and dropped sets must partition the wire field set exactly, so a new
proto field fails in every language until somebody classifies it.

## Context

Four languages each hand-wrote a `WorkloadEnvelope` and no build step compared
any of them with the wire message or with each other. Go matched the proto. Rust
declared `inputs: Vec<BufferDescriptor>` and `expected_output_digests:
Vec<Digest>` where the wire declares `repeated ArtifactRef inputs` and
`repeated ArtifactRef expected_outputs`, and Python carried a fifth shape.

The Rust divergence was not a spelling difference. An `ArtifactRef` is content
identity that outlives every lease; a `BufferDescriptor` is a segment, a lease
expiry and a transport that mean nothing to the control plane. Using one field
name for both made them indistinguishable at the one seam that has to tell them
apart, and `services/node_agent` called the divergent type "the canonical
workload envelope" in its own doc comment. Nothing decoded the message, so
nothing failed -- the first decoder written against the wire would have had to
invent the difference.

Separating the two also made an absent check expressible. The node validated
that each buffer's lease was live and then read it, so a buffer for content the
envelope never listed was indistinguishable from one it did. There was no
authorized set to check against while the envelope carried the descriptors
itself.

## Consequences

- `libs/rust/worker_protocol` gains an `ArtifactRef` view of
  `mindclade.common.v1.ArtifactRef` and its `WorkloadEnvelope` matches the wire
  field for field.
- `NodeAgentCore::execute_workload` takes materialized buffers as a sibling
  argument and calls `WorkloadEnvelope::bind_materialized`, which refuses an
  unauthorized buffer before it is read.
- Rust cross-checks all seven identities the envelope duplicates from its signed
  ticket rather than three, matching Go.
- Python keeps its narrower process-local projection. It holds the ticket's
  identity rather than the signed ticket, because Rust has already verified
  execution authority before an engine runs and the engine must not hold a
  credential it cannot use; its stage fields are delegated to `StageEnvelope`,
  which projects `StageSpec`, mirroring `StageAttempt.spec` on the wire.
- A language that needs a node-local or process-local shape gives it its own
  name. Reusing `WorkloadEnvelope` for something that is not the wire message is
  the defect this record forbids.
