//! Tenant-scoped, content-addressed artifact data-plane core.
//!
//! The proxy owns byte-path authorization, range validation, digest verification,
//! bounded caching, and transfer accounting. Artifact catalog and tenant policy
//! remain Go control-plane responsibilities.
#![forbid(unsafe_code)]

pub mod access;
pub mod cache;
pub mod config;
pub mod grants;
pub mod health;
pub mod lifecycle;
pub mod provider;
pub mod ranges;
pub mod server;
pub mod signing;
pub mod telemetry;
pub mod transfer;
pub mod verification;
pub use access::AccessContext;
pub use cache::{CacheEntry, LocalCache};
pub use config::ProxyConfig;
pub use grants::ValidatedGrant;
pub use health::{ProxyHealth, ProxyHealthSnapshot};
pub use lifecycle::ArtifactProxyComponent;
pub use ranges::RangeRequest;
pub use server::{ArtifactProxyCore, ReadResult, WriteResult};
pub use signing::{DownloadClaim, SignedDownload};
pub use telemetry::ProxyMetrics;
pub use provider::ProviderArtifacts;
