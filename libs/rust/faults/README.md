# `mindclade_faults`

Stable, structured errors for Rust libraries and process boundaries.

## Guarantees

- Stable snake-case error codes.
- Explicit retry hints.
- Deterministically ordered context.
- Sensitive values are never retained in the fault value.
- Default display output is safe for operational logs.

This crate deliberately avoids transport-specific status objects. Adapters map `Code`
to gRPC, HTTP, or local protocol status at the edge.
