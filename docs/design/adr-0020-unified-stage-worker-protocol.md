# ADR-0020: Use one ticketed stage-worker protocol

- **Status:** Accepted
- **Date:** 2026-08-13

## Decision

Ingestion, curation, preprocessing, reference building, batch inference,
evaluation, training, checkpoint/artifact transfer, rollout and simulation use
one durable stage vocabulary. Go owns the DAG and attempt state; Rust owns
signed-ticket/resource/process execution; Python owns scientific/numerical
engines. Inputs and outputs are immutable artifact identities.

The common protocol standardizes authority, fencing, retries, deadlines,
artifacts and status without forcing unrelated scientific engines into one
implementation.
