# PostgreSQL outbox repository

This package exposes the production PostgreSQL outbox adapter through the
original storage namespace. It delegates to the canonical fenced implementation
under `coordination/outbox/postgres` and therefore shares the same table schema,
claim transitions, transaction propagation, error qualification, and tests.

`AppendInTransaction` is the paved-road helper for atomically mutating domain
state and appending an outbox envelope. Claiming and dispatch must occur outside
caller transactions.
