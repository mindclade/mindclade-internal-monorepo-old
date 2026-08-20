# Mindclade Go Clock

`go.mindclade.dev/libs/go/clock` provides a minimal injectable clock for
production code and a deterministic manually advanced clock for tests.

## Production use

```go
func NewLeaseManager(source clock.Clock) *LeaseManager {
    if source == nil {
        source = clock.RealClock{}
    }
    return &LeaseManager{clock: source}
}
```

Prefer constructor injection. Do not introduce a mutable process-global clock.

## Context-aware sleep

```go
if err := clock.Sleep(ctx, retryDelay); err != nil {
    return err
}
```

`Sleep` returns `ctx.Err()` when cancellation wins. It does not implement retry
policy or backoff; those belong in `libs/go/retry`.

## Deterministic tests

```go
start := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
fake := clock.NewFake(start)

done := make(chan error, 1)
go func() {
    done <- fake.Sleep(context.Background(), time.Minute)
}()

if err := fake.BlockUntil(ctx, 1); err != nil {
    t.Fatal(err)
}
if err := fake.Advance(time.Minute); err != nil {
    t.Fatal(err)
}
if err := <-done; err != nil {
    t.Fatal(err)
}
```

`FakeClock.Advance` and `Set` never permit time to move backward. Tickers use a
single buffered delivery and may drop intermediate ticks when a large advance
crosses multiple periods, matching the standard library's slow-consumer
semantics.

## Boundaries

This package owns:

- current-time access;
- timers and tickers;
- context-aware sleeping;
- deterministic manual time in tests.

It does not own:

- retry policy or jitter;
- cron or workflow scheduling;
- service startup and shutdown;
- lease semantics;
- persistence.

The package has no third-party or Mindclade-library dependencies and should not
contain a nested `go.mod` in the monorepo.
