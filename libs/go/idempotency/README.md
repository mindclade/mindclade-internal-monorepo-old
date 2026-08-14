# Idempotency

`idempotency` defines replay protection for write operations. A client-provided
key is isolated inside a server-controlled scope and bound to a SHA-256 request
fingerprint.

```go
execution, err := executor.Execute(ctx, idempotency.AcquireRequest{
    Identity:      identity,
    Fingerprint:   identifiers.SHA256(canonicalRequest),
    TTL:           24 * time.Hour,
    LeaseDuration: time.Minute,
}, func(ctx context.Context) (idempotency.Result, error) {
    return idempotency.NewResult(responseBytes, "application/json", nil)
})
```

A store must atomically return one of four dispositions: acquired, replay,
in-progress, or conflict. Completion, release, and renewal compare both lease
token and version so stale workers cannot commit reclaimed work.

`idempotencytest.MemoryStore` is deterministic and concurrency-safe but not
durable. Production SQL or Redis implementations belong in storage adapters.

`Executor` deliberately does not renew leases automatically. Set a lease longer
than the bounded operation, or use `Store.Acquire`/`Store.Renew` directly for
long-running work. Result completion and failure cleanup use a short, bounded
finalization context that survives cancellation of the inbound request.
