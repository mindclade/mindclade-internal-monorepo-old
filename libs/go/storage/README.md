# Storage

`storage` is a namespace for precise, capability-specific persistence contracts.
It deliberately does not define a universal `Store` abstraction.

- `blob` models immutable or conditionally replaced byte objects.
- `cache` models bounded ephemeral key/value data with optimistic versions.
- `lease` models fenced ownership with renewable expirations.
- `sql/transaction` provides context-aware `database/sql` transaction helpers.
- `sql/postgres` classifies PostgreSQL SQLSTATE failures and configures pools.

Provider implementations live beneath the contract they implement. Service-specific
repositories and business schemas remain with their owning service.
