# sql/postgres

Driver-neutral PostgreSQL helpers for `database/sql`. Error classification uses
the standard `SQLState() string` contract implemented by modern PostgreSQL
drivers, avoiding a hard dependency on pgx or lib/pq.
