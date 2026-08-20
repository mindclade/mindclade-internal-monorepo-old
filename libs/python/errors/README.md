# Python fault taxonomy

Layer 0 of `libs/python`. Standard library only; every other package here may
import it and it imports none of them.

## What it owns

A `MindcladeError` carries a stable `Code`, a client-safe message, an optional
machine-readable `reason` and `operation`, bounded diagnostic `fields`, explicit
`RetryPolicy` intent, and an optional wrapped `cause`.

The seventeen codes are the same strings `libs/go/faults/code.go` and
`libs/rust/faults/src/code.rs` declare. That is the point of the package: a fault
crossing a process boundary has to classify the same on both sides.
`tests/integration/cross_language/test_error_codes.py` asserts the Python and Go
sets are equal, so they cannot drift silently.

## What it does not own

No transport, logging, telemetry, persistence, workflow runtime, or PyTorch. It
records retry *intent* and never retries anything — choosing whether, when and
how to try again belongs to the caller, the same split `libs/go/faults` and
`libs/go/retry` keep.

## Disclosure

`str(fault)` is a diagnostic and includes the wrapped cause. Do not send it to an
external caller. Transports serialize `code_of`, `public_message_of`,
`reason_of`, and a reviewed subset of `fields_of`.

`public_message_of` returns the generic message for any exception that is not a
`MindcladeError`, rather than that exception's own text: arbitrary exception
strings are not vetted for disclosure, and the whole purpose of the accessor is
that its result is safe to send outward.

## Limits and failure behavior

`fields` is bounded at 128 entries, 128-character keys and 4096-character values,
matching the envelope bounds in `libs/python/worker_runtime`. Exceeding a bound
raises at construction rather than truncating, so an oversized diagnostic is a
visible programming error rather than a silently shortened log line.

`parse_code` raises on an unrecognized code so version skew surfaces;
`normalize_code` degrades to `unknown` for callers that would rather continue.
`is_retryable` is fail-closed: an error carrying no explicit intent is not
retryable.

`InvalidArgument` and `FailedPrecondition` also inherit `ValueError` so a package
can adopt the taxonomy without breaking a consumer that still catches the
built-in. `DeadlineExceeded` cannot do the same for `TimeoutError`, which
descends from `OSError` and has an incompatible instance layout.

`OutOfRange` also preserves `ValueError` compatibility and is used when a
well-formed numeric contract reaches its wire maximum. Client-safe messages are
bounded at 4096 characters, reasons at 128, and operation names at 256; oversized
diagnostics fail at construction rather than creating unbounded frames or logs.
