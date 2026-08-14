# Transactional outbox

`outbox` is the repository-wide mechanism for publishing durable events after a
state mutation without using distributed transactions.

The service mutation and `Store.Append` run in the same
`storage/sql/transaction` context. A `Dispatcher` claims rows with fencing,
publishes at least once, and acknowledges only with the current claim token and
version. Every dispatcher is registered through `Dispatcher.Component`, so
`servicekit` owns startup, readiness, cancellation, and shutdown.

Use it for run lifecycle events, registry promotion events, webhook delivery,
ingestion snapshot publication, evaluation completion, and audit export. Do
not implement service-local outbox tables or polling loops.
