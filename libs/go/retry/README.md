# retry

`retry` executes bounded, context-aware retry policies for Mindclade Go
services and infrastructure adapters.

The package does not infer that every transient-looking error is safe to retry.
By default, an error must carry an explicit retryable `faults.RetryPolicy`.
Callers may install a classifier for operations with separately proven replay
safety.

```go
policy, err := retry.NewPolicy(
    retry.WithMaxAttempts(5),
    retry.WithMaxElapsed(30*time.Second),
)
if err != nil {
    return err
}

executor, err := retry.NewExecutor(policy)
if err != nil {
    return err
}

result, err := executor.Do(ctx, "artifact.publish", func(ctx context.Context, attempt retry.Attempt) error {
    return publisher.Publish(ctx, artifact)
})
```

## Guarantees

- total-attempt and elapsed-time bounds;
- context cancellation and deadlines;
- fixed, immediate, and exponential backoff;
- symmetric bounded jitter with deterministic injection for tests;
- explicit `RetryKindNever`, immediate, delayed, and backoff semantics;
- server-provided minimum retry delays;
- retry lifecycle observers with panic containment;
- immutable execution summaries;
- structured failures through `libs/go/faults`;
- no third-party dependencies.

The operation itself must be replay-safe or protected by idempotency. Retrying a
non-idempotent operation without those guarantees is a correctness bug.
