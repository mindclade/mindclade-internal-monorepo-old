# ADR-0013: Durable ingestion and scientific preprocessing planes

- **Status:** Accepted
- **Date:** 2026-08-13
- **Scope:** Data ingestion and full-pipeline biological prediction

## Context

External ingestion, MSA generation, template search, ligand preparation, and
featurization can require long CPU/high-memory jobs, large reference databases,
retries, caching, and independent scaling. Running them inside the online
inference request path would hold network sessions and GPUs while non-GPU work
executes.

## Decision

These workloads are durable staged workflows:

- Go owns source snapshots, DAG/state, attempts, global scheduling, quotas,
  database-snapshot selection, tickets, cancellation intent, and publication.
- Rust owns bounded byte transfer/parsing, reference-cache activation, external
  tool/process supervision, node budgets, artifact/checkpoint transfer, and
  local diagnostics.
- Python owns scientific curation, entity normalization, MSA filtering/pairing,
  template selection, ligand/CCD semantics, feature construction, and
  scientific provenance.

Prepared-input inference may enter the online runtime directly. Full-pipeline
prediction first commits a validated immutable `PreprocessedInputBundle`, then
requests GPU inference. GPUs are never reserved while MSA/template work waits.

## Consequences

Expensive stages are independently schedulable, cacheable, retryable, and
reusable. Reference databases and cache keys are immutable/versioned. Clients
observe a durable run ID rather than relying on one long-held request.

## Rejected alternatives

MSA/template search in the Rust gateway, scientific semantics in Go/Rust, and
GPU reservation before model-ready inputs were rejected.
