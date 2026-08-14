# lease/postgres

PostgreSQL fenced lease store using `database/sql`, atomic `ON CONFLICT`
reacquisition, token/version checks, and driver-neutral SQLSTATE mapping.
Lease acquisition, expiration, renewal, and retry-delay calculations use
PostgreSQL server time rather than application-host clocks.
