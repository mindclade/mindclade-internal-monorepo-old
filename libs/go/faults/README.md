# Mindclade Go Faults

`mindclade.internal/libs/go/faults` is Mindclade's transport-neutral structured-error package for Go services and libraries.

It provides:

- stable machine-readable error codes;
- safe public messages separated from wrapped diagnostic causes;
- machine-readable reasons and operation names;
- defensively copied structured fields with conservative secret redaction;
- explicit retry intent without embedding a retry engine;
- request, trace, and operation metadata carried through `context.Context`;
- classification helpers that work through ordinary Go error wrapping.

The package depends only on the Go standard library. It must remain below HTTP, gRPC, Connect, logging, telemetry, database, scheduler, and service-contract packages in the dependency graph.

## Import

```go
import "mindclade.internal/libs/go/faults"
```

Do not add a `go.mod` inside this package when it lives in the Mindclade monorepo. Use the repository's authoritative Go module or workspace.

## Constructing faults

```go
err := faults.New(
    faults.CodeFailedPrecondition,
    "training run cannot be promoted",
    faults.WithReason("qualification_incomplete"),
    faults.WithOperation("releases.Promote"),
    faults.WithField(faults.FieldRunID, runID),
    faults.WithRetryPolicy(faults.NoRetry()),
)
```

`New` returns an `error` so callers are encouraged to classify through the stable helper API rather than depend on the concrete `*Fault` representation.

## Wrapping causes

```go
record, err := repository.Load(ctx, runID)
if err != nil {
    return faults.Wrap(
        err,
        faults.CodeUnavailable,
        "unable to load training run",
        faults.WithOperation("runs.Repository.Load"),
        faults.WithRetryPolicy(faults.BackoffRetry(5)),
    )
}
```

`errors.Is` and `errors.As` continue to work because `Fault` implements `Unwrap`.

`Fault.Error()` includes the operation and wrapped cause for logs and diagnostics. External transports must not serialize `err.Error()` directly. Use `faults.PublicMessageOf`, `faults.CodeOf`, `faults.ReasonOf`, and `faults.FieldsOf` when building client responses.

## Classification

```go
switch faults.CodeOf(err) {
case faults.CodeNotFound:
    // Handle absence.
case faults.CodeUnavailable:
    // Apply the caller's retry engine to the explicit retry policy.
default:
    // Escalate or convert at the boundary.
}

if faults.IsReason(err, "qualification_incomplete") {
    // Present a domain-specific remediation.
}
```

The outermost structured fault owns classification. Wrapping a deadline error in a `CodeUnavailable` fault intentionally classifies the result as unavailable.

Unstructured standard-library errors receive a small set of conservative mappings:

- `context.Canceled` → `canceled`;
- `context.DeadlineExceeded` → `deadline_exceeded`;
- `fs.ErrNotExist` → `not_found`;
- `fs.ErrExist` → `already_exists`;
- `fs.ErrPermission` → `permission_denied`;
- `fs.ErrInvalid` → `invalid_argument`.

Everything else is `unknown`.

## Public versus diagnostic messages

```go
clientPayload := struct {
    Code      faults.Code `json:"code"`
    Message   string      `json:"message"`
    RequestID string      `json:"request_id,omitempty"`
}{
    Code:      faults.CodeOf(err),
    Message:   faults.PublicMessageOf(err),
    RequestID: requestID,
}
```

For a structured fault, `PublicMessageOf` returns the explicitly supplied message and never appends the cause. For an unstructured error, it returns the canonical message for the inferred code rather than exposing the raw error text.

## Context metadata

```go
ctx = faults.ContextWithRequestID(ctx, requestID)
ctx = faults.ContextWithTraceID(ctx, traceID)
ctx = faults.ContextWithOperation(ctx, "runs.Create")

err := faults.New(
    faults.CodeInternal,
    "unable to create run",
    faults.WithContextMetadata(ctx),
)
```

Context metadata is a fallback. Explicit options already present on the fault take precedence when `WithContextMetadata` is applied.

Do not use this package as a general identity context. Principals, organizations, tenants, entitlements, and authorization decisions belong in dedicated packages. Identifiers may be attached as structured diagnostic fields after the relevant policy permits it.

## Fields and redaction

```go
err := faults.New(
    faults.CodeInvalidArgument,
    "invalid model request",
    faults.WithFields(faults.Fields{
        faults.FieldModelID: "clade-1",
        "parameter":         "sequence",
        "api_key":           apiKey, // Stored as "[REDACTED]".
    }),
)
```

Field maps are copied on insertion and retrieval. Common nested maps and slices are copied recursively. Unknown mutable value types should be treated as immutable by callers.

The redaction filter is intentionally conservative but is not a substitute for correct data handling. Never attach secrets, credentials, raw request or response bodies, model inputs, biological datasets, or large payloads to errors.

## Retry intent

```go
faults.NoRetry()
faults.ImmediateRetry(3)
faults.BackoffRetry(5)
faults.DelayedRetry(30*time.Second, 5)
```

`MaxAttempts` means the maximum total attempts, including the initial attempt. Zero delegates the limit to the caller's policy. The package describes retry intent; it does not sleep, schedule, jitter, or execute retries.

`IsRetryable` relies only on an explicit retry policy. It does not infer retries from a code such as `unavailable`, because the safety and idempotency of retrying are operation-specific.

## Transport adapters

Keep transport mappings outside this package:

```text
libs/go/httpx/faults.go
libs/go/grpcx/faults.go
libs/go/connectx/faults.go
```

The allowed dependency direction is:

```text
faults ← httpx/grpcx/connectx ← services
```

The core package must never import a transport framework.

## Bazel

The included `BUILD.bazel` exports:

```text
//libs/go/faults:faults
//libs/go/faults:faults_test
```

Adjust the target path only if the package is placed elsewhere. The import path is fixed to:

```text
mindclade.internal/libs/go/faults
```
