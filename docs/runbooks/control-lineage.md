# Runbook: control lineage

Serves the `control/lineage` package — the immutable provenance graph that binds source revisions,
datasets, training runs, checkpoints, model bundles, evaluations, and release evidence to a
deployment subject.

## Scope note, stated plainly

`lineage.Service` is not constructed anywhere outside this package today; the only references to
`control/lineage` elsewhere in the tree are unrelated uses of the word "lineage" for request
tracing. This runbook therefore covers a package with no production caller yet. It is written from
the code's actual failure surface so that it is correct when a caller appears, and it should not be
read as evidence that provenance is being recorded in production.

## Trigger

Lineage publication is rejected, a stored graph fails to read back, or a release is blocked by an
incomplete provenance requirement.

## Failure modes and what each one means

### `lineage_digest_mismatch` — stored bytes do not hash to the digest they are filed under

`Get` recomputes the digest of the graph it read and compares it to the digest requested
(`control/lineage/service.go:45-58`). A mismatch is raised as `CodeDataLoss` with `NoRetry`
(`:49-56`). This is the serious one: it means the repository returned bytes that are not the graph
that digest names.

- Do **not** retry; the fault is marked no-retry because retrying cannot help.
- Do not republish over the digest. The repository contract requires `Put` to be idempotent for the
  same digest and to reject binding different bytes to it (`control/lineage/repository.go:14-15`);
  a mismatch means either that contract was violated or the stored bytes were corrupted.
- Quarantine the digest, preserve the stored bytes, and treat every release decision that consumed
  that graph as unproven until re-derived. Follow `artifact-corruption.md` for the byte-level
  handling.

### `lineage_release_incomplete` — a required provenance kind is absent or disconnected

`ReleaseRequirements.Validate` walks edges **backwards** from the node whose digest equals
`graph.SubjectDigest`, building the ancestor set, then requires at least one node of each required
kind to be in that set (`control/lineage/service.go:84-119`). Two different situations produce this
one reason:

- the required kind has no node in the graph at all; or
- a node of that kind exists but is not an ancestor of the subject — it is in the graph without
  being connected to what is being released.

The second is easy to misdiagnose as the first. Check for the node's presence before concluding it
was never produced. A disconnected node usually means an edge was omitted by the producer, and the
fix is the missing edge, not a weakened requirement. Do not satisfy this check by removing the
required kind from `RequiredKinds`.

### `lineage_requirements_invalid`, `lineage_requirement_kind_invalid`, `lineage_requirement_duplicate`

Requirements must be non-empty, no larger than the node count, drawn from the fifteen valid
`NodeKind` values (`control/lineage/graph.go:24-38`), and free of duplicates
(`control/lineage/service.go:71,76,79`). These are caller defects, not data corruption.

### `lineage_graph_cycle` — the graph is not acyclic

Acyclicity is enforced during validation (`control/lineage/graph.go:161`). Edges run input to
output, so a cycle means a producer emitted a provenance claim that makes an artifact its own
ancestor. Fix the producer.

### `lineage_repository_unavailable`

`Publish` and `Get` both refuse a nil repository (`control/lineage/service.go:21-23,35-37`). This is
a wiring fault, reported as `CodeFailedPrecondition` with `NoRetry`.

### Bounds exceeded

`MaximumNodes` is 4096 and `MaximumEdges` is 16384 (`control/lineage/graph.go:17-18`). A graph that
exceeds either is rejected. These are deliberate bounds; the response is to reduce graph
granularity at the producer, never to raise the constant to make one release pass.

## Confidentiality note

`Node` carries only immutable identity — locations, credentials, presigned URLs, raw samples,
prompts, and model bytes deliberately cannot be represented in it
(`control/lineage/graph.go:70-71`). When exporting to a metadata system, `MLflowProjection` drops
every node classified `restricted` and every edge touching one, and reports the count it withheld
(`control/lineage/service.go:134-168`). If an operator needs the full graph, read it from the
repository; do not widen the projection.

## Exit criteria

The graph validates, its digest reads back consistently, required provenance kinds are connected to
the release subject, and no bound, requirement, or classification filter was relaxed to get there.

## Known limitations recorded here deliberately

- No metrics, logs, or traces are emitted by `control/lineage`. Every signal above is a fault
  reason observed by the caller.
- There is no durable `Repository` implementation in the repository; the interface
  (`control/lineage/repository.go:16-19`) is unimplemented outside tests.
