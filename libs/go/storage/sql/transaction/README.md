# sql/transaction

Minimal `database/sql` transaction runner with guaranteed rollback on operation
failure and structured begin/commit/rollback faults. Automatic transaction
retries are deliberately outside this package.
