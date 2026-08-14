//! Tokio/Axum online network edge for the Rust runtime gateway.
//!
//! This layer owns network framing and bounded graceful shutdown only. The
//! framework-independent [`GatewayCore`] remains the sole local
//! admission/routing authority on the node.

use crate::{protocol, GatewayCore};
use axum::body::Bytes;
use axum::extract::{DefaultBodyLimit, State};
use axum::http::{header::CONTENT_TYPE, HeaderValue, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use axum::Router;
use mindclade_faults::{Code, Fault};
use mindclade_protocols::runtime::v1::{RuntimeDispatchRequest, RuntimeDispatchResponse};
use prost::Message;
use std::future::IntoFuture;
use std::io;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;
use tokio::net::TcpListener;
use tokio::sync::{oneshot, watch};
use tokio::time::timeout;
use tower::limit::ConcurrencyLimitLayer;

const PROTOBUF_CONTENT_TYPE: &str = "application/x-protobuf";
const MAX_DISPATCH_BYTES: usize = 1024 * 1024;
const MAX_NETWORK_CONCURRENCY: usize = 8_192;
const GATEWAY_DRAIN_TIMEOUT: Duration = Duration::from_secs(30);

#[derive(Clone)]
pub struct GatewayNetworkState {
    core: Arc<GatewayCore>,
}

impl GatewayNetworkState {
    #[must_use]
    pub fn new(core: Arc<GatewayCore>) -> Self {
        Self { core }
    }
}

/// Serve the public runtime edge with a bounded graceful-drain interval.
///
/// Once `shutdown` is asserted the listener stops accepting new connections.
/// Existing connections receive at most [`GATEWAY_DRAIN_TIMEOUT`] to finish;
/// exceeding that deadline fails the server rather than leaving an unowned
/// task tree alive indefinitely.
pub async fn serve(
    address: SocketAddr,
    state: GatewayNetworkState,
    mut shutdown: watch::Receiver<bool>,
) -> Result<(), io::Error> {
    let listener = TcpListener::bind(address).await?;
    let app = Router::new()
        .route("/healthz", get(healthz))
        .route("/readyz", get(readyz))
        .route("/v1/runtime/dispatch", post(dispatch))
        .layer(DefaultBodyLimit::max(MAX_DISPATCH_BYTES))
        .layer(ConcurrencyLimitLayer::new(MAX_NETWORK_CONCURRENCY))
        .with_state(state);

    let (drain_tx, drain_rx) = oneshot::channel::<()>();
    let mut drain_tx = Some(drain_tx);
    let server = axum::serve(listener, app)
        .with_graceful_shutdown(async move {
            let _ = drain_rx.await;
        })
        .into_future();
    tokio::pin!(server);

    loop {
        tokio::select! {
            result = &mut server => return result,
            changed = shutdown.changed() => {
                if changed.is_err() || *shutdown.borrow() {
                    if let Some(sender) = drain_tx.take() {
                        let _ = sender.send(());
                    }
                    break;
                }
            }
        }
    }

    match timeout(GATEWAY_DRAIN_TIMEOUT, &mut server).await {
        Ok(result) => result,
        Err(_) => Err(io::Error::new(
            io::ErrorKind::TimedOut,
            "runtime gateway exceeded graceful-drain deadline",
        )),
    }
}

async fn healthz() -> StatusCode {
    StatusCode::OK
}

async fn readyz(State(state): State<GatewayNetworkState>) -> StatusCode {
    if state.core.health_snapshot().ready() {
        StatusCode::OK
    } else {
        StatusCode::SERVICE_UNAVAILABLE
    }
}

async fn dispatch(State(state): State<GatewayNetworkState>, body: Bytes) -> Response {
    if body.len() > MAX_DISPATCH_BYTES {
        return fault_response(Fault::new(
            Code::ResourceExhausted,
            "runtime dispatch message exceeds network framing bound",
        ));
    }

    let message = match RuntimeDispatchRequest::decode(body.as_ref()) {
        Ok(message) => message,
        Err(error) => {
            return fault_response(
                Fault::invalid_argument("runtime dispatch protobuf is invalid").with_source(error),
            );
        }
    };
    let request = match protocol::inference_request(message) {
        Ok(request) => request,
        Err(error) => return fault_response(error),
    };
    let now = match unix_millis() {
        Ok(now) => now,
        Err(error) => return fault_response(error),
    };
    let admitted = match state.core.admit_request(request, now) {
        Ok(admitted) => admitted,
        Err(error) => return fault_response(error),
    };

    let response = RuntimeDispatchResponse {
        request_id: admitted.request_id.to_string(),
        deployment_id: admitted.route.deployment_id.to_string(),
        endpoint: admitted.route.endpoint.clone(),
    };

    // The edge admission permit is released after route selection. Runtime-host
    // execution acquires a separate node resource reservation for the request.
    admitted.permit.release();
    protobuf_response(StatusCode::ACCEPTED, response.encode_to_vec())
}

fn protobuf_response(status: StatusCode, body: Vec<u8>) -> Response {
    let mut response = (status, body).into_response();
    response
        .headers_mut()
        .insert(CONTENT_TYPE, HeaderValue::from_static(PROTOBUF_CONTENT_TYPE));
    response
}

/// Render only the stable public code and the explicitly non-secret fault
/// message. Structured context and source errors remain internal telemetry and
/// are never reflected to an untrusted client.
fn fault_response(error: Fault) -> Response {
    let status = match error.code() {
        Code::InvalidArgument | Code::OutOfRange => StatusCode::BAD_REQUEST,
        Code::Unauthenticated => StatusCode::UNAUTHORIZED,
        Code::PermissionDenied => StatusCode::FORBIDDEN,
        Code::ResourceExhausted => StatusCode::TOO_MANY_REQUESTS,
        Code::Unavailable => StatusCode::SERVICE_UNAVAILABLE,
        Code::DeadlineExceeded => StatusCode::GATEWAY_TIMEOUT,
        _ => StatusCode::INTERNAL_SERVER_ERROR,
    };
    let body = format!("{}: {}", error.code().as_str(), error.message());
    (status, body).into_response()
}

fn unix_millis() -> Result<u64, Fault> {
    let elapsed = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_err(|_| {
            Fault::new(
                Code::FailedPrecondition,
                "runtime gateway clock is before Unix epoch",
            )
        })?;
    u64::try_from(elapsed.as_millis())
        .map_err(|_| Fault::new(Code::OutOfRange, "runtime gateway clock exceeds u64 milliseconds"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn public_fault_response_does_not_expose_context_or_source() {
        let fault = Fault::internal("request failed")
            .with_context("tenant", "tenant-secret")
            .with_sensitive_context("credential")
            .with_source(io::Error::other("provider secret"));
        let response = fault_response(fault);
        assert_eq!(response.status(), StatusCode::INTERNAL_SERVER_ERROR);

        let body = axum::body::to_bytes(response.into_body(), 4_096)
            .await
            .expect("public fault response body");
        let body = std::str::from_utf8(&body).expect("public fault response UTF-8");
        assert_eq!(body, "internal: request failed");
        assert!(!body.contains("tenant-secret"));
        assert!(!body.contains("provider secret"));
        assert!(!body.contains("credential"));
    }
}
