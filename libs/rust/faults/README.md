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

## Cross-language contract

`Code` mirrors `mindclade.common.v1.ErrorCode`
(`protocols/proto/mindclade/common/v1/errors.proto`, the wire authority) and `faults.Code`
in `libs/go/faults`. `tests/integration/cross_language/test_error_codes.py` fails if any of
the three drifts.

- **Canonical spellings.** `Cancelled` and `Unimplemented` serialize as `canceled` and
  `not_implemented` — the spellings the Go control plane already emits on
  `Mindclade-Error-Code` and into telemetry. `cancelled` and `unimplemented` are still
  accepted on parse; both mirrors accept both and emit one.
- **Total ingestion.** `Code::from_wire` degrades an unrecognized peer code to
  `Code::Unknown`. `FromStr` stays strict so a typo in configuration fails loudly, and it
  rejects on length before allocating. A peer running a newer build can never make this one
  fail to read a fault.
- **Retry guidance.** `WireRetryKind` travels beside `retry_after_millis` because a delay
  alone cannot separate an explicit `Never` from silence, and cannot express the Go control
  plane's `with_backoff`. `WithBackoff` maps to `RetryHint::Immediate` in memory — this
  crate's `Immediate` already means "retry subject to the caller's own schedule".

### Known limit on the emit side

`RetryHint` has three states, so `WireFault::from(&Fault)` can only emit `after`,
`immediate`, or `never` — never `unspecified` or `with_backoff`. A relay that ingests a
peer's `with_backoff` through `WireFault::to_fault` and re-projects the `Fault` emits
`immediate`; forward the `WireFault` itself instead. Likewise `Fault::new` derives `Never`
from any non-transient code, so a default-constructed fault asserts `never` rather than
staying silent. Closing both requires `RetryHint::{Unspecified, Backoff}`, which is a
source-breaking change for the exhaustive match in `libs/rust/data_stream` and is therefore
tracked separately.
