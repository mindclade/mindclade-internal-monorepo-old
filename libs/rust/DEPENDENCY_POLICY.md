# Rust Dependency Policy

Dependencies are admitted by class rather than prohibited globally.

- **Foundation:** std plus narrowly audited deterministic crates; keep minimal.
- **Runtime:** curated Tokio/Tower/Tonic/Prost/Bytes/telemetry dependencies.
- **Adapter:** provider cloud, compression, Python, GPU, and system dependencies.

Any third-party crate requires an owner, Bzlmod/Cargo lock entries, license and
checksum metadata, SBOM/provenance coverage, compatibility tests, security review,
and rollback path. Provider SDKs must not leak into domain-neutral contracts.
