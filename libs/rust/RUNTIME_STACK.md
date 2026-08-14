# Rust runtime stack

Production service adapters standardize on one async stack: Tokio for execution,
Tonic/Prost for generated control-plane gRPC, Tower for bounded middleware, Bytes
for network buffers, and tracing/OpenTelemetry behind `telemetry`. These are
service/runtime dependencies, not foundation requirements for deterministic
pure contracts. The offline scaffold does not invent lockfile entries it cannot
resolve; connected Bazel/Nix qualification pins and audits exact versions before
promotion.

Rules: one Tokio runtime per process; no detached tasks; bounded blocking pools;
all queues bounded; all waits cancellable/deadlined; shutdown bounded; large
payloads use `BufferDescriptor` rather than protobuf bytes.
