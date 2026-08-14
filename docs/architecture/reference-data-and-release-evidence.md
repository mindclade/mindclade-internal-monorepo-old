# Reference data, artifact identity, and release evidence

## Artifact identity

An immutable artifact is identified by content and contract, not by a cloud
path:

```text
ArtifactRef
  digest
  size_bytes
  media_type
  logical_kind
  schema_version

ArtifactLocation
  artifact_ref/digest
  provider
  URI
  provider generation/version
  optional region
```

The same `ArtifactRef` concept can represent dataset shards, MSA output,
template hits, feature bundles, checkpoint shards, model/runtime bundles,
evaluation results, reference-database shards and release evidence. Moving or
replicating bytes changes locations, never identity.

## Reference database release

MSA/template/annotation search inputs are promoted immutable releases rather
than mutable directories on nodes. A release records source/cutoff versions,
shard artifact identities, index format, generating tool/version, compatible
search tools, licensing/provenance and a content-bound snapshot digest. Rust
node/cache code activates the exact snapshot requested by a signed stage ticket;
Python scientific provenance records that digest in outputs.

A model prediction is therefore reproducible against:

```text
model bundle digest
+ resolved preprocessing config digest
+ reference database snapshot digest(s)
+ search/toolchain provenance
```

## Evidence graph

A production model/runtime release is not “qualified because a script exited
zero.” `control/registry/releases` represents evidence as a DAG. Nodes are
immutable evidence artifacts (source/build/training/checkpoint/model/kernel/
evaluation/safety/runtime/SBOM/provenance/signature/etc.); edges state the
claimed derivation or qualification relationship. Promotion validates:

- required evidence kinds are present;
- the graph is acyclic and canonical;
- evidence is bound to the intended release subject/policy epoch;
- the graph digest is deterministic;
- the current production policy accepts the graph.

Provider-specific evidence producers remain separate. The registry validates
and promotes evidence; it does not fabricate it.
