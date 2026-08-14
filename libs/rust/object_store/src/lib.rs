//! Conditional object-store contracts, verified namespace clients, and provider-neutral adapters.
#![forbid(unsafe_code)]

pub mod adapters;
pub mod client;
pub mod conditional;
pub mod config;
mod local;
pub mod metrics;
mod memory;
pub mod multipart;
pub mod namespace;
mod path;
pub mod range;
pub mod retry;
mod store;
pub mod verification;
pub use client::Client;
pub use config::ClientConfig;
pub use local::LocalStore;
pub use memory::MemoryStore;
pub use metrics::StoreMetrics;
pub use namespace::Namespace;
pub use path::{ObjectPath, ObjectPathError};
pub use store::{ObjectMeta, ObjectStore, PutCondition, PutResult};
