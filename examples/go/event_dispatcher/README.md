# Durable event dispatcher example

This runnable integration demonstrates the canonical process composition:

1. A domain mutation appends an immutable message to the outbox store.
2. `coordination/outbox.Dispatcher` claims it with a fenced lease.
3. An adapter publishes it through the broker-neutral `messaging` contract.
4. The broker delivers it to a bounded subscription.
5. The dispatcher marks the outbox record published.
6. `servicekit/production` drains and stops every component in reverse order.

The example uses memory providers so it runs without external infrastructure.
A production dispatcher swaps in PostgreSQL and Pub/Sub adapters without
changing the coordination or lifecycle contracts.

```bash
go run ./examples/go/event_dispatcher
go test ./examples/go/event_dispatcher
```
