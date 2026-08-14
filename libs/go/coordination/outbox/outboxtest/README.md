# Outbox conformance testing

`outboxtest` is the reusable provider-conformance suite for durable outbox
stores. Production and in-memory adapters run the same claim, fencing,
publication, retry, completion, and stale-owner tests so service code does not
encode provider-specific assumptions.

This package is test-only and must never be imported by production targets.
