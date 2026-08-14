# Data ingestion

The ingestion system converts mutable external sources into immutable,
reproducible, lineage-rich dataset inputs. It covers PDB, UniProt, RNAcentral,
object stores, HTTP sources, partner drops, and future adapters without placing
source-specific policy in shared infrastructure libraries.

## Data layers

```text
raw          exact source bytes plus source metadata
canonical    parsed, normalized, schema-valid records
curated      deduplicated, licensed, quality/safety-screened records
model-ready  deterministic shards, indices, tokenizer/features, manifests
```

Each layer is immutable and digest-addressed. A later transformation can be
replayed without redownloading the source.

## Pipeline

```text
1. Go connector discovers source snapshot and durable cursor
2. Go ingestion controller creates an idempotent stage DAG and execution tickets
3. Rust ingestion/node worker fetches, resumes, decompresses, and bounds bytes
4. Rust format adapters frame records and commit raw artifacts
5. Python normalizes scientific meaning and emits canonical records
6. Python curation performs deduplication, licensing, contamination, quality,
   privacy, and biological-safety checks
7. Python/Rust loader tooling builds deterministic model-ready shards
8. Go registry records lineage, evidence, and publishes the dataset version
```

## Language ownership

| Concern | Owner |
|---|---|
| Source snapshot, cursor, stage state, quotas, retries, leases, publication | Go |
| Transfer, decompression, bounded parsing, local cache, record framing | Rust |
| Biological normalization, curation, featurization, quality semantics | Python |
| Artifact bytes and digest verification | Rust artifact plane |

## Stage contract

Every stage has an immutable input manifest, output schema, resource envelope,
deadline, idempotency key, retry classification, fencing token, tool/database
versions, diagnostics location, and terminal result. Large outputs are artifacts,
not broker payloads.

## Correctness rules

- source snapshot identity includes source version, retrieval policy, and cutoff;
- cursors advance only in the same transaction as accepted effects;
- stale workers cannot commit after lease replacement;
- retries are safe only when the stage contract explicitly declares replay;
- partial raw transfers are resumable but never published as complete;
- canonicalization and curation versions are captured in lineage;
- dataset publication is a separate gated transition.

## Repository mapping

```text
data/connectors/       source-specific adapters and contracts
data/ingestion/        reusable stage semantics
control/ingestion/     durable policy and workflow state
services/workers/ingestion/ thin execution adapter
libs/rust/*            transfer, bounded parse, records, object store
libs/go/coordination/* durable control mechanisms
```
