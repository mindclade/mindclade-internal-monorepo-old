// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Mindclade online inference runtime gateway core.
//!
//! This crate intentionally implements the latency-sensitive *local* request
//! path without embedding global policy or a network framework. The Go control
//! plane publishes signed bounded authority; this core verifies and consumes
//! that authority locally. A Tokio/Tonic/Tower edge adapter is a leaf concern.
#![forbid(unsafe_code)]

pub mod admission;
pub mod auth;
pub mod authority;
pub mod bootstrap;
pub mod config;
pub mod grpc;
pub mod health;
pub mod host_probe;
pub mod lifecycle;
pub mod network;
pub mod protocol;
pub mod routing;
pub mod server;
pub mod streaming;
pub mod telemetry;
pub mod tickets;
pub use admission::{AdmissionLedger, AdmissionPermit, AdmissionRequest};
pub use authority::GatewayAuthority;
pub use config::GatewayConfig;
pub use health::{GatewayHealth, GatewayHealthSnapshot};
pub use lifecycle::GatewayComponent;
pub use routing::{RouteRequest, select_route};
pub use server::{AdmittedRequest, GatewayCore, InferenceEnvelope};
pub use streaming::{ResponseChunk, StreamReceiver, StreamSender, bounded_stream};
pub use tickets::{PolicyCache, PolicySnapshot};
