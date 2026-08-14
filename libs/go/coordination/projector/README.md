# Event projector runtime

`projector` is a bounded servicekit component that composes a source adapter,
transactional inbox, monotonic cursor, and domain handler.

```text
receive delivery
  -> validate envelope and ordering position
  -> inbox transaction
       -> apply handler effects
       -> advance cursor
       -> optional outbox append
  -> commit
  -> acknowledge delivery
```

The package owns loop, backoff, drain, and correctness mechanics. The consumer
owns event schema and domain effects. Register it as
`production.CapabilityProjector`.
