# PostgreSQL transactional outbox

The adapter uses PostgreSQL `clock_timestamp()`, `FOR UPDATE SKIP LOCKED`, and
claim token/version fencing. `Append` joins the current
`storage/sql/transaction` context so a service mutation and its event commit
atomically. Claiming inside a caller transaction is rejected because delivery
claims must be visible before publication begins.
