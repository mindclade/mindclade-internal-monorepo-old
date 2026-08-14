# Scientific preprocessing

Preprocessing is a durable, cacheable, separately schedulable plane between raw
biological requests/datasets and model execution. It supports NovaFold-style
structure prediction as well as chemistry, multimodal, sequence, and general
scientific pipelines.

## Why it is separate

MSA generation, template search, reference lookups, ligand preparation, and
feature construction may consume minutes or hours, high CPU/RAM, and large
local databases. They must not hold an online gateway request slot or reserve a
GPU model slot while waiting.

## Generic pipeline model

```text
canonical input
  -> entity decomposition and deduplication
  -> cache lookup
  -> parallel reference/search/chemistry stages
  -> joins and pairing
  -> scientific featurization
  -> immutable PreprocessedInputBundle
  -> GPU inference or training input
```

## Stage ownership

- Go owns the durable DAG, attempts, leases, quotas, resource class, database
  snapshot selection, cancellation intent, and terminal state.
- Rust owns node-local reference caches, digest verification, external process
  supervision, CPU/RAM/disk limits, workspace cleanup, and artifact transfer.
- Python owns scientific policy, parsing semantics, filtering, pairing,
  selection, ligand chemistry, feature layout, and provenance.

## Stage descriptor

Every stage descriptor includes input artifact refs, output namespace, schema
version, executable/tool bundle, reference snapshot IDs, resource budget,
deadline, retry/idempotency policy, fencing token, environment digest, and
expected diagnostics.

## Cache hierarchy

Cache raw search output, parsed results, filtered/selected results, joined
results, and final feature bundles independently. Keys include all scientific
policy, tool, schema, and reference-snapshot versions. A cache hit never omits
provenance.

## Scheduling pools

```text
search CPU/high-memory/NVMe
featurization CPU/high-memory
chemistry CPU
GPU inference
training/multinode GPU
artifact and reference transfer
```

Interactive work uses warm leased workers; bulk generation and backfills use
Kubernetes Jobs/JobSets with Kueue admission.

## Repository mapping

```text
preprocessing/contracts/  scientific stage and bundle contracts
preprocessing/pipeline/   planner/executor/resume semantics
preprocessing/biology/    MSA, templates, pairing, structure features
preprocessing/chemistry/  CCD, ligands, conformers, bonds
preprocessing/cache/      cache-key and promotion semantics
services/workers/preprocessing/ deployable adapter
services/node_agent/      local cache/process/transfer enforcement
control/orchestration/    durable workflow state
```
