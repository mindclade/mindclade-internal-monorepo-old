# Transactional inbox

`inbox` composes the existing `idempotency.Store` with an explicit transaction
runner so an at-least-once event is applied once per consumer/handler version.

```go
processor, err := inbox.New(sqlRunner, idempotencyStore)
outcome, err := processor.Process(ctx, inbox.Message{
    Identity:      identity,
    Fingerprint:   payloadDigest,
    RequestID:     requestID,
    TTL:           30 * 24 * time.Hour,
    LeaseDuration: time.Minute,
}, func(txCtx context.Context) (idempotency.Result, error) {
    // Apply domain effects, advance cursor, append downstream outbox using txCtx.
    return result, nil
})
```

The same identity with a different payload digest is a protocol-integrity
conflict. Acknowledge broker delivery only after the transaction commits.
