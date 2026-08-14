//! Reusable Rust serving data-plane contracts.
//!
//! This crate owns request envelopes, local admission, immutable policy-cache
//! semantics, deterministic routing, bounded response streaming, coarse batch
//! compatibility, and host/gateway port contracts. Network adapters and model
//! numerics remain in deployable Rust services and Python workers respectively.
#![forbid(unsafe_code)]

pub mod admission;
pub mod batch_envelope;
pub mod gateway;
pub mod host;
pub mod load_shed;
pub mod request;
pub mod response;
pub mod routing;
pub mod snapshot;
pub mod streaming;
pub mod supervision;
pub mod telemetry;
pub mod ticket;
pub use admission::{AdmissionLedger, AdmissionPermit, AdmissionRequest};
pub use batch_envelope::{BatchCompatibilityKey, BatchEnvelope};
pub use load_shed::{LoadShedDecision, LoadShedder};
pub use request::InferenceRequest;
pub use response::{InferenceResponse, ResponseChunk};
pub use routing::{RouteRequest, select_route};
pub use snapshot::{PolicyCache, PolicySnapshot};
pub use streaming::{StreamReceiver, StreamSender, bounded_stream};
