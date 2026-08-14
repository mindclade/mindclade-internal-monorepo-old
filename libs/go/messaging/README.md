# Messaging

`messaging` is the narrow at-least-once publisher/subscription/delivery contract
used by outbox dispatchers, ingestion coordinators, projectors, and admin
processors. It owns validation, publication acknowledgements, delivery attempts,
ack deadlines, settlement, cancellation, and bounded receive loops.

```go
message, err := messaging.NewMessage(id, topic, orderingKey, contentType,
    payload, headers, requestMetadata, createdAt)
publication, err := publisher.Publish(ctx, message)

err = subscription.Receive(ctx, func(ctx context.Context, d messaging.Delivery) error {
    // Pair with inbox/projector for durable effects.
    return d.Ack(ctx)
})
```

It does not own domain schemas, workflow state, business retry policy, or
exactly-once claims. `memory` is deterministic/local; `pubsub` is the production
provider boundary and must pass shared conformance.
