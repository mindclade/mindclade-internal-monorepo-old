// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Strict, provenance-carrying service configuration for Rust processes.
//!
//! This is the Rust counterpart of `libs/go/config`, and it exists because
//! there was no Rust counterpart: `mindclade_servicekit::ServiceConfig` holds
//! three lifecycle timeouts and never reads the environment, so every Rust
//! service grew its own loader. Three of them had independently reimplemented
//! the same `required` / `parse_u64` / `absolute_path` helper set, with three
//! different redaction stories and three different answers to what an empty
//! value means.
//!
//! What it provides, matching the Go package feature for feature:
//!
//! - ordered [`Source`]s, later sources winning
//! - a [`Field`] catalog carrying required/default/secret/reloadable metadata
//! - **unknown-key rejection**, failing closed the way `libs/go/config` does
//! - per-value provenance ([`Origin`], [`Provenance`])
//! - a deterministic [`Snapshot::digest`] that never contains a secret
//! - redaction through the [`Secret`] type and [`Snapshot::redacted`]
//! - atomic last-known-good reload with restart-required reporting
//!   ([`AtomicConfig`])
//!
//! and one thing the Go package does not: **mandatory field documentation**, so
//! [`Catalog::documentation`] can always render the whole surface. That is the
//! usable half of the idea behind Cloudflare's `foundations` `settings` module —
//! the configuration surface should be visible rather than implied by scattered
//! `unwrap_or` calls — taken without the dependency or a proc-macro.
//!
//! # No ambient state
//!
//! `libs/rust/SECURITY.md` requires that foundation crates create no ambient
//! async runtime, global thread pool, or hidden provider client. Configuration
//! is where that rule is usually broken, by a global that reads the environment
//! on first touch. There is no global here: a [`Catalog`] is built explicitly
//! and [`Catalog::load`] is called by a composition root. Nothing in this crate
//! reads the process environment unless an [`EnvSource::process`] is handed to
//! a load.
//!
//! # Example
//!
//! ```
//! use mindclade_config::{Catalog, EnvSource, Field};
//! use std::collections::BTreeMap;
//!
//! let catalog = Catalog::new("example")?
//!     .declare(Field::required("service.name", "Process identity."))?
//!     .declare(
//!         Field::required("service.token", "Upstream credential.").secret(),
//!     )?
//!     .declare(
//!         Field::defaulted("log.level", "Emitted log verbosity.", "info").reloadable(),
//!     )?;
//!
//! let environment = EnvSource::from_table(BTreeMap::from([
//!     ("EXAMPLE_NAME".to_owned(), "scheduler".to_owned()),
//!     ("EXAMPLE_TOKEN".to_owned(), "hunter2".to_owned()),
//! ]))
//! .bind("service.name", "EXAMPLE_NAME")
//! .bind("service.token", "EXAMPLE_TOKEN")
//! .bind("log.level", "EXAMPLE_LOG_LEVEL");
//!
//! let snapshot = catalog.load(&[&environment])?;
//! assert_eq!(snapshot.raw("service.name")?, "scheduler");
//! assert_eq!(snapshot.raw("log.level")?, "info");
//! assert_eq!(snapshot.redacted()["service.token"], "[REDACTED]");
//! assert_eq!(format!("{:?}", snapshot.secret("service.token")?), "[REDACTED]");
//! # Ok::<(), mindclade_faults::Fault>(())
//! ```
#![forbid(unsafe_code)]

mod catalog;
mod error;
mod field;
mod reload;
mod secret;
mod snapshot;
mod source;

pub use catalog::{Catalog, DEFAULT_SOURCE, MAX_FIELDS, MAX_NAMESPACE_BYTES, MAX_SOURCES};
pub use error::reason;
pub use field::{
    EmptyValue, Field, MAX_DOC_BYTES, MAX_KEY_BYTES, MAX_VALUE_BYTES, Whitespace, is_canonical_key,
};
pub use reload::AtomicConfig;
pub use secret::{REDACTED, Secret};
pub use snapshot::{Origin, Provenance, Snapshot};
pub use source::{EnvSource, MAX_SOURCE_ENTRIES, MAX_VARIABLE_BYTES, MapSource, Source};
