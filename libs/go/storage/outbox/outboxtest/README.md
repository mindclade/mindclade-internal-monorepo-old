# Outbox conformance tests

This test-only package exposes the canonical outbox conformance suite and a
deterministic in-memory repository through the storage-facing package path.
Provider adapters must pass the same lifecycle, lease-expiry, and fencing tests.
