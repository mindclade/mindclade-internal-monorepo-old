# Durable cursors

`cursor` stores monotonic source or projection positions with compare-and-swap
versions and fencing. Use it for ingestion source checkpoints and ordered event
projectors.

A caller reads the current cursor, applies effects in a transaction, and calls
`Advance` with the expected version, nondecreasing sequence, opaque provider
position, and current fence. Regression, stale fence, and version conflict fail
explicitly. Use the memory conformance adapter in tests and PostgreSQL adapter
in production.
