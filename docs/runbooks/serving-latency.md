# Runbook: serving latency regression

## Trigger

Qualified latency or time-to-first-output exceeds threshold for a model,
hardware, batch class, or request shape.

## Triage

Partition by route/model bundle, runtime bundle, hardware, input shape, batch
class, kernel provider/signature, compilation mode, cache state, and stage.
Compare admission wait, Python batch wait, preprocessing (if any), model
execution, sampling/diffusion, confidence/ranking, and streaming.

## Recovery

- Unqualified kernel/compiler regression: revoke the signature and fall back to
  PyTorch/reference.
- Batch policy regression: reduce coarse admission class or Python batch
  limits; Python remains final tensor-batch authority.
- Cold model/cache: verify deployment warmup and capacity rather than hiding the
  issue with unbounded queues.
- Preprocessing: move requests to durable full-pipeline flow; do not hold online
  connections/GPU slots for MSA or template search.
- Capacity shortage: apply Go global policy/scheduling and Rust local load
  shedding.

## Exit criteria

Latency returns below threshold across the qualified matrix without violating
memory, numerical, safety, or cancellation gates, and the regression is covered
by a benchmark/evidence comparison.
