# Pub/Sub messaging adapter

This package implements the canonical at-least-once provider adapter without
forcing the cloud SDK into every consumer. A service composition root wraps its
pinned provider Topic and Subscription types with the narrow facades in
`provider.go`; all message validation, reserved attributes, request lineage,
concurrency bounds, settlement, retries, and shutdown behavior remain shared.

It does not define domain event schemas, handler retry safety, inbox policy,
ordering guarantees beyond the provider ordering key, or exactly-once claims.
