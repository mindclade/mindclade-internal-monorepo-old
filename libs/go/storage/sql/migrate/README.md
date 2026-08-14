# PostgreSQL migrations

The migration runner provides one production mechanism for Go-owned schemas:
strictly increasing forward migrations, SHA-256 checksums, unknown-version and
checksum-drift rejection, a session-scoped PostgreSQL advisory lock, one
transaction per migration, durable receipts, dry planning, and servicekit
startup integration.

Migration SQL remains under the service that owns the tables. The library does
not provide an ORM or automatic destructive rollback; schema changes follow
expand/contract release policy.
