// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Tokio/Axum online network edge for the Rust runtime gateway.
//!
//! This layer owns network framing and bounded graceful shutdown only. The
//! framework-independent [`GatewayCore`] remains the sole local
//! admission/routing authority on the node.

use crate::{GatewayCore, protocol};
use axum::Router;
use axum::body::{Bytes, to_bytes};
use axum::extract::{DefaultBodyLimit, FromRequest, Request, State};
use axum::http::{HeaderValue, StatusCode, header::CONTENT_LENGTH, header::CONTENT_TYPE};
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use mindclade_faults::{Code, Fault, status};
use mindclade_protocols::runtime::v1::{RuntimeDispatchRequest, RuntimeDispatchResponse};
use mindclade_runtime_core::{BytePermit, ByteSemaphore, ByteSemaphoreSnapshot};
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
const BYTE_ADMISSION_GRANULARITY: u64 = 4 * 1024;

#[derive(Clone, Debug)]
pub struct GatewayNetworkState {
    core: Arc<GatewayCore>,
    request_bytes: Arc<ByteSemaphore>,
    response_bytes: Arc<ByteSemaphore>,
    execution_enabled: bool,
}

impl GatewayNetworkState {
    pub fn new(
        core: Arc<GatewayCore>,
        request_buffer_budget_bytes: u64,
        response_buffer_budget_bytes: u64,
        execution_enabled: bool,
    ) -> Result<Self, Fault> {
        Ok(Self {
            core,
            request_bytes: ByteSemaphore::new(request_buffer_budget_bytes)?,
            response_bytes: ByteSemaphore::new(response_buffer_budget_bytes)?,
            execution_enabled,
        })
    }

    #[must_use]
    pub fn memory_snapshot(&self) -> (ByteSemaphoreSnapshot, ByteSemaphoreSnapshot) {
        (
            self.request_bytes.snapshot(),
            self.response_bytes.snapshot(),
        )
    }

    pub(crate) fn reserve_request_bytes(&self, bytes: u64) -> Result<BytePermit, Fault> {
        self.request_bytes.try_acquire(bytes)
    }

    pub(crate) fn reserve_response_bytes(&self, bytes: u64) -> Result<BytePermit, Fault> {
        self.response_bytes.try_acquire(bytes)
    }

    #[must_use]
    pub(crate) const fn execution_enabled(&self) -> bool {
        self.execution_enabled
    }

    pub(crate) fn core(&self) -> &Arc<GatewayCore> {
        &self.core
    }
}

#[derive(Debug)]
struct AdmittedBody {
    bytes: Bytes,
    _memory: BytePermit,
}

impl FromRequest<GatewayNetworkState> for AdmittedBody {
    type Rejection = Response;

    async fn from_request(
        request: Request,
        state: &GatewayNetworkState,
    ) -> Result<Self, Self::Rejection> {
        let declared = match declared_body_bytes(&request) {
            Ok(value) => value,
            Err(error) => return Err(fault_response(&error)),
        };
        let reserved = round_admission_bytes(declared).map_err(|error| fault_response(&error))?;
        let mut memory = state
            .request_bytes
            .try_acquire(reserved)
            .map_err(|error| fault_response(&error))?;
        let bytes = to_bytes(request.into_body(), MAX_DISPATCH_BYTES)
            .await
            .map_err(|error| {
                fault_response(
                    &Fault::new(
                        Code::ResourceExhausted,
                        "runtime request exceeds network framing bound",
                    )
                    .with_source(error),
                )
            })?;
        let actual = u64::try_from(bytes.len()).map_err(|_| {
            fault_response(&Fault::new(
                Code::OutOfRange,
                "runtime request length exceeds u64",
            ))
        })?;
        if actual > declared && declared != u64::try_from(MAX_DISPATCH_BYTES).unwrap_or(u64::MAX) {
            return Err(fault_response(&Fault::invalid_argument(
                "runtime request exceeds declared content length",
            )));
        }
        memory
            .shrink_to(round_admission_bytes(actual).map_err(|error| fault_response(&error))?)
            .map_err(|error| fault_response(&error))?;
        Ok(Self {
            bytes,
            _memory: memory,
        })
    }
}

/// Serve the public runtime edge with a bounded graceful-drain interval.
///
/// Once `shutdown` is asserted the listener stops accepting new connections.
/// Existing connections receive at most `GATEWAY_DRAIN_TIMEOUT` to finish;
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
        .route("/v1/runtime/resolve", post(resolve))
        .route("/v1/runtime/dispatch", post(dispatch_compat))
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

async fn resolve(State(state): State<GatewayNetworkState>, body: AdmittedBody) -> Response {
    resolve_request(&state, &body, StatusCode::OK)
}

async fn dispatch_compat(State(state): State<GatewayNetworkState>, body: AdmittedBody) -> Response {
    let mut response = resolve_request(&state, &body, StatusCode::ACCEPTED);
    response
        .headers_mut()
        .insert("deprecation", HeaderValue::from_static("true"));
    response.headers_mut().insert(
        "link",
        HeaderValue::from_static("</v1/runtime/resolve>; rel=\"successor-version\""),
    );
    response
}

fn resolve_request(
    state: &GatewayNetworkState,
    body: &AdmittedBody,
    success_status: StatusCode,
) -> Response {
    let message = match RuntimeDispatchRequest::decode(body.bytes.as_ref()) {
        Ok(message) => message,
        Err(error) => {
            return fault_response(
                &Fault::invalid_argument("runtime dispatch protobuf is invalid").with_source(error),
            );
        }
    };
    let request = match protocol::inference_request(message) {
        Ok(request) => request,
        Err(error) => return fault_response(&error),
    };
    let now = match unix_millis() {
        Ok(now) => now,
        Err(error) => return fault_response(&error),
    };
    let admitted = match state.core.admit_request(request, now) {
        Ok(admitted) => admitted,
        Err(error) => return fault_response(&error),
    };

    let response = RuntimeDispatchResponse {
        request_id: admitted.request_id.to_string(),
        deployment_id: admitted.route.deployment_id.to_string(),
        endpoint: admitted.route.endpoint.clone(),
    };

    // The edge admission permit is released after route selection. Runtime-host
    // execution acquires a separate node resource reservation for the request.
    admitted.permit.release();
    protobuf_response(success_status, response.encode_to_vec())
}

fn declared_body_bytes(request: &Request) -> Result<u64, Fault> {
    let maximum = u64::try_from(MAX_DISPATCH_BYTES)
        .map_err(|_| Fault::new(Code::OutOfRange, "body limit exceeds u64"))?;
    let Some(value) = request.headers().get(CONTENT_LENGTH) else {
        return Ok(maximum);
    };
    let value = value
        .to_str()
        .map_err(|error| Fault::invalid_argument("content length is invalid").with_source(error))?
        .parse::<u64>()
        .map_err(|error| Fault::invalid_argument("content length is invalid").with_source(error))?;
    if value > maximum {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "runtime request exceeds network framing bound",
        ));
    }
    Ok(value)
}

pub(crate) fn round_admission_bytes(bytes: u64) -> Result<u64, Fault> {
    if bytes == 0 {
        return Ok(0);
    }
    bytes
        .checked_add(BYTE_ADMISSION_GRANULARITY - 1)
        .map(|value| value / BYTE_ADMISSION_GRANULARITY * BYTE_ADMISSION_GRANULARITY)
        .ok_or_else(|| Fault::new(Code::OutOfRange, "byte admission rounding overflow"))
}

fn protobuf_response(status: StatusCode, body: Vec<u8>) -> Response {
    let mut response = (status, body).into_response();
    response.headers_mut().insert(
        CONTENT_TYPE,
        HeaderValue::from_static(PROTOBUF_CONTENT_TYPE),
    );
    response
}

/// Render only the stable public code and the explicitly non-secret fault
/// message. Structured context and source errors remain internal telemetry and
/// are never reflected to an untrusted client.
fn fault_response(error: &Fault) -> Response {
    let status = http_status(error.code());
    let body = format!("{}: {}", error.code().as_str(), error.message());
    (status, body).into_response()
}

/// Renders the canonical HTTP status for a fault code.
///
/// The table is `mindclade_faults::status`, which mirrors `libs/go/httpx`. This
/// gateway used to keep its own `match` and disagreed with
/// `services/ai_gateway_proxy` about seven codes, `NotFound` among them: a
/// missing deployment answered 500, which told the client to retry and paged an
/// operator for a request that would never succeed.
///
/// `from_u16` is fallible only outside 100..1000 and the canonical table yields
/// nothing there — `mindclade_faults::status` asserts every code renders in
/// 400..600 — so the fallback is unreachable rather than a policy.
fn http_status(code: Code) -> StatusCode {
    StatusCode::from_u16(status::http_status(code)).unwrap_or(StatusCode::INTERNAL_SERVER_ERROR)
}

pub(crate) fn unix_millis() -> Result<u64, Fault> {
    let elapsed = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_err(|_| {
            Fault::new(
                Code::FailedPrecondition,
                "runtime gateway clock is before Unix epoch",
            )
        })?;
    u64::try_from(elapsed.as_millis()).map_err(|_| {
        Fault::new(
            Code::OutOfRange,
            "runtime gateway clock exceeds u64 milliseconds",
        )
    })
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
        let response = fault_response(&fault);
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

    /// Every fault code renders the canonical HTTP status.
    ///
    /// The numbers are restated here rather than read back from
    /// `mindclade_faults::status`. Reading them back would assert only that
    /// this edge calls the shared function, and the defect being closed is two
    /// edges that each called nothing shared: these values are the
    /// client-visible contract and a change to any one of them has to break a
    /// test that names it.
    #[test]
    fn every_fault_code_renders_its_canonical_http_status() {
        let expected: &[(Code, u16)] = &[
            (Code::InvalidArgument, 400),
            (Code::OutOfRange, 400),
            (Code::Unauthenticated, 401),
            (Code::PermissionDenied, 403),
            (Code::NotFound, 404),
            (Code::Cancelled, 408),
            (Code::AlreadyExists, 409),
            (Code::Conflict, 409),
            (Code::Aborted, 409),
            (Code::FailedPrecondition, 412),
            (Code::ResourceExhausted, 429),
            (Code::Internal, 500),
            (Code::DataLoss, 500),
            (Code::Unknown, 500),
            (Code::Unimplemented, 501),
            (Code::Unavailable, 503),
            (Code::DeadlineExceeded, 504),
        ];
        assert_eq!(
            expected.len(),
            status::ALL.len(),
            "a fault code is missing from this table"
        );
        for &(code, want) in expected {
            let rendered = fault_response(&Fault::new(code, "rendered")).status();
            assert_eq!(rendered.as_u16(), want, "{code} rendered HTTP {rendered}");
        }
    }

    /// The regression. This gateway answered 500 for `NotFound`, so a client
    /// asking for a deployment that does not exist was told the server had
    /// failed: retry, and page an operator, for a request that can never
    /// succeed. `services/ai_gateway_proxy` answered 404 for the same fault.
    #[test]
    fn a_missing_resource_is_not_reported_as_a_server_failure() {
        let response = fault_response(&Fault::new(Code::NotFound, "no such deployment"));
        assert_eq!(response.status(), StatusCode::NOT_FOUND);
    }
}
