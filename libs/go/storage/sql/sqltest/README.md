# SQL test driver

`sqltest` provides a deterministic `database/sql` driver for exercising
transaction boundaries, rollback, cancellation, and provider error mapping
without a live database. It is test-only and must never be imported by
production targets.
