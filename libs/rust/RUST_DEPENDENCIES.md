# Rust Dependency Inventory

The foundation has no crates.io or Git dependencies. `Cargo.lock` contains only
workspace path packages. This minimizes supply-chain exposure and keeps local,
remote-execution, and recovery binaries reproducible.

Connected provider services may use admitted dependencies, but those dependencies
must not leak into `libs/rust` without a dependency-policy review.
