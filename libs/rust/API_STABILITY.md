# Rust API Stability

- **stable** crates require additive changes or an approved compatibility epoch.
- **evolving** crates require migration tests and affected-consumer qualification.
- **compatibility** crates are temporary facades and may only lose APIs through a
  repository-wide migration.

Persisted formats are versioned independently from Rust type layout. A breaking
format change requires dual read, migration tooling, rollback support, and a
release evidence bundle. `cargo public-api` baselines are generated in connected
CI; `public_api_baseline.json` records the admitted crate inventory offline.
