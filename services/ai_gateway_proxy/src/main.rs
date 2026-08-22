// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

#![forbid(unsafe_code)]

use mindclade_ai_gateway_proxy::{
    AppState, GoogleIdTokenVerifier, HttpControlPlane, HttpProvider, ProxyConfig, router,
};
use mindclade_faults::{Code, Fault, FaultResult};
use std::sync::Arc;

#[tokio::main]
async fn main() {
    if let Err(error) = run().await {
        eprintln!("mindclade-ai-gateway-proxy failed: {}", error.message());
        std::process::exit(78);
    }
}

async fn run() -> FaultResult<()> {
    let config = Arc::new(ProxyConfig::from_env()?);
    let control = Arc::new(HttpControlPlane::new(
        config.control_base_url.clone(),
        config.control_token.clone(),
        config.control_timeout,
    )?);
    let provider = Arc::new(HttpProvider::new(
        config.provider_timeout,
        &config.egress_proxy_url,
        &config.egress_ca_bundle_path,
    )?);
    let verifier = GoogleIdTokenVerifier::new(
        config.client_audience.clone(),
        config.google_jwks_url.clone(),
        config.control_timeout,
    )?;
    verifier.prewarm().await?;
    let state = AppState::new(config.clone(), control, provider, Arc::new(verifier));
    state.mark_ready();
    let listener = tokio::net::TcpListener::bind(config.listen_address)
        .await
        .map_err(|error| {
            Fault::new(Code::Unavailable, "AI Gateway listener bind failed").with_source(error)
        })?;
    axum::serve(listener, router(state))
        .with_graceful_shutdown(shutdown_signal())
        .await
        .map_err(|error| {
            Fault::new(Code::Internal, "AI Gateway HTTP server failed").with_source(error)
        })
}

async fn shutdown_signal() {
    let _ = tokio::signal::ctrl_c().await;
}
