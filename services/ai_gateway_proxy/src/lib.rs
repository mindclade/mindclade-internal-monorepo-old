// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Governed OpenAI-compatible provider proxy.
//!
//! The Go control plane owns policy and durable budget state. This service
//! owns only bounded request transport, provider egress, and the explicit
//! reserve/dispatch/reconcile protocol. Request and response bodies are never
//! emitted as telemetry or trace attributes.
#![forbid(unsafe_code)]

pub mod auth;
pub mod config;
pub mod control;
pub mod model;
pub mod provider;
pub mod server;
pub mod telemetry;

pub use auth::{GoogleIdTokenVerifier, Identity, IdentityVerifier};
pub use config::{EndpointConfig, ProxyConfig};
pub use control::{ControlPlane, HttpControlPlane};
pub use model::{
    EndpointPolicy, GatewayOperation, ProviderResult, Quota, ReservationDecision, ResolvedEndpoint,
};
pub use provider::{HttpProvider, Provider};
pub use server::{AppState, router};
