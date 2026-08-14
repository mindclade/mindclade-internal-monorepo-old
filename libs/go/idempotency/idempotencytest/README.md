# Idempotency Test Store

`idempotencytest.MemoryStore` implements the complete atomic lease state machine
for unit tests and local development. It accepts injectable clocks and
identifier generators. It is intentionally not durable and must not be used as
a production idempotency store.
