// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::{
    ControlPlane, GatewayOperation, IdentityVerifier, Provider, ProxyConfig,
    control::ReserveInput,
    model::{ProviderResult, ReservationDecision, ResolvedEndpoint},
    telemetry::GatewayMetrics,
};
use axum::{
    Router,
    body::Bytes,
    extract::{DefaultBodyLimit, State},
    http::{HeaderMap, HeaderName, HeaderValue, StatusCode, header},
    response::{IntoResponse, Response},
    routing::{get, post},
};
use mindclade_content_digest::hash_bytes;
use mindclade_faults::{Code, Fault, FaultResult, status};
use serde_json::{Value, json};
use std::{
    sync::{
        Arc,
        atomic::{AtomicBool, Ordering},
    },
    time::Instant,
};
use tokio::sync::Semaphore;

const HEADER_WORKSPACE: &str = "x-mindclade-workspace-id";
const HEADER_IDEMPOTENCY: &str = "idempotency-key";

#[derive(Clone, Debug)]
pub struct AppState {
    pub config: Arc<ProxyConfig>,
    pub control: Arc<dyn ControlPlane>,
    pub provider: Arc<dyn Provider>,
    pub verifier: Arc<dyn IdentityVerifier>,
    pub metrics: GatewayMetrics,
    concurrency: Arc<Semaphore>,
    ready: Arc<AtomicBool>,
}

impl AppState {
    #[must_use]
    pub fn new(
        config: Arc<ProxyConfig>,
        control: Arc<dyn ControlPlane>,
        provider: Arc<dyn Provider>,
        verifier: Arc<dyn IdentityVerifier>,
    ) -> Self {
        Self {
            concurrency: Arc::new(Semaphore::new(config.maximum_concurrency)),
            config,
            control,
            provider,
            verifier,
            metrics: GatewayMetrics::default(),
            ready: Arc::new(AtomicBool::new(false)),
        }
    }

    pub fn mark_ready(&self) {
        self.ready.store(true, Ordering::Release);
    }
}

pub fn router(state: AppState) -> Router {
    let maximum_body_bytes = state.config.maximum_body_bytes;
    Router::new()
        .route("/", get(livez))
        .route("/livez", get(livez))
        .route("/readyz", get(readyz))
        .route("/metrics", get(metrics))
        .route("/v1/chat/completions", post(chat_completions))
        .route("/v1/responses", post(responses))
        .route("/v1/embeddings", post(embeddings))
        .layer(DefaultBodyLimit::max(maximum_body_bytes))
        .with_state(state)
}

async fn livez() -> StatusCode {
    StatusCode::OK
}

async fn readyz(State(state): State<AppState>) -> StatusCode {
    if state.ready.load(Ordering::Acquire) {
        StatusCode::OK
    } else {
        StatusCode::SERVICE_UNAVAILABLE
    }
}

async fn metrics(State(state): State<AppState>) -> Response {
    let mut response = state.metrics.prometheus().into_response();
    response.headers_mut().insert(
        header::CONTENT_TYPE,
        HeaderValue::from_static("text/plain; version=0.0.4; charset=utf-8"),
    );
    response
        .headers_mut()
        .insert(header::CACHE_CONTROL, HeaderValue::from_static("no-store"));
    response
}

async fn chat_completions(
    State(state): State<AppState>,
    headers: HeaderMap,
    body: Bytes,
) -> Response {
    execute(state, headers, body, GatewayOperation::ChatCompletions).await
}

async fn responses(State(state): State<AppState>, headers: HeaderMap, body: Bytes) -> Response {
    execute(state, headers, body, GatewayOperation::Responses).await
}

async fn embeddings(State(state): State<AppState>, headers: HeaderMap, body: Bytes) -> Response {
    execute(state, headers, body, GatewayOperation::Embeddings).await
}

async fn execute(
    state: AppState,
    headers: HeaderMap,
    body: Bytes,
    operation: GatewayOperation,
) -> Response {
    let started = Instant::now();
    let result = execute_inner(&state, &headers, body, operation).await;
    let _elapsed = started.elapsed();
    match result {
        Ok(response) => response,
        Err(error) => {
            state.metrics.rejected();
            fault_response(&error)
        }
    }
}

async fn execute_inner(
    state: &AppState,
    headers: &HeaderMap,
    body: Bytes,
    operation: GatewayOperation,
) -> FaultResult<Response> {
    let _permit = state.concurrency.clone().try_acquire_owned().map_err(|_| {
        Fault::new(
            Code::ResourceExhausted,
            "AI Gateway concurrency limit reached",
        )
    })?;
    if !state.ready.load(Ordering::Acquire) {
        return Err(Fault::new(Code::Unavailable, "AI Gateway is not ready"));
    }
    let token = bearer_token(headers)?;
    let identity = state.verifier.verify(token).await?;
    let subject = identity.policy_subject();
    let request = prepare_request(state, headers, &body, operation)?;
    let endpoint = request.endpoint;
    let governed = state
        .control
        .resolve(&subject, &request.workspace, &endpoint.alias, operation)
        .await?;
    verify_governed_endpoint(endpoint, &governed, operation)?;
    let reserved = state
        .control
        .reserve(ReserveInput {
            subject: &subject,
            request_digest: &request.request_digest,
            idempotency_key: &request.idempotency_key,
            workspace: &request.workspace,
            route: &governed.endpoint.route,
            policy_epoch: governed.policy_epoch,
            requested: &governed.subject_maximum_request,
            ttl_seconds: state.config.reservation_ttl_seconds,
        })
        .await?;
    if reserved.reservation.state != "reserved" {
        return Err(Fault::new(
            Code::Conflict,
            "idempotent request is already in progress or terminal",
        ));
    }
    if reserved.reservation.workspace != request.workspace
        || reserved.reservation.route != governed.endpoint.route
        || reserved.reservation.policy_epoch != governed.policy_epoch
        || reserved.reservation.reserved != governed.subject_maximum_request
    {
        let _ = state
            .control
            .release(&subject, &reserved, &request.request_digest)
            .await;
        return Err(Fault::data_loss(
            "control-plane reservation does not match the requested endpoint policy",
        ));
    }
    state.metrics.accepted();
    let dispatched = state
        .control
        .dispatch(&subject, &reserved, &request.request_digest)
        .await?;
    if dispatched.reservation.state != "dispatched" {
        return Err(Fault::data_loss(
            "control-plane dispatch transition returned an invalid state",
        ));
    }
    state.metrics.dispatched();
    let provider_result = match state
        .provider
        .invoke(
            endpoint,
            operation,
            body,
            state.config.maximum_response_bytes,
        )
        .await
    {
        Ok(result) => result,
        Err(error) => {
            mark_pending(state, &subject, &dispatched, &request.request_digest).await;
            return Err(error);
        }
    };
    terminalize(
        state,
        &subject,
        endpoint,
        &dispatched,
        &request.request_digest,
        &provider_result,
    )
    .await?;
    provider_response(provider_result)
}

struct PreparedRequest<'a> {
    endpoint: &'a crate::EndpointConfig,
    workspace: String,
    idempotency_key: String,
    request_digest: String,
}

fn prepare_request<'a>(
    state: &'a AppState,
    headers: &HeaderMap,
    body: &Bytes,
    operation: GatewayOperation,
) -> FaultResult<PreparedRequest<'a>> {
    let workspace = single_header(headers, HEADER_WORKSPACE, 512)?.to_owned();
    let idempotency_key = single_header(headers, HEADER_IDEMPOTENCY, 256)?.to_owned();
    if idempotency_key.len() < 8 {
        return Err(Fault::invalid_argument("idempotency key is too short"));
    }
    let document: Value = serde_json::from_slice(body).map_err(|error| {
        Fault::invalid_argument("OpenAI-compatible request body is invalid").with_source(error)
    })?;
    let object = document.as_object().ok_or_else(|| {
        Fault::invalid_argument("OpenAI-compatible request body must be an object")
    })?;
    if object
        .get("stream")
        .and_then(Value::as_bool)
        .unwrap_or(false)
    {
        return Err(Fault::new(
            Code::Unimplemented,
            "streaming is fail-closed pending qualification",
        ));
    }
    let alias = object
        .get("model")
        .and_then(Value::as_str)
        .filter(|value| !value.is_empty() && value.len() <= 256)
        .ok_or_else(|| Fault::invalid_argument("model alias is required"))?;
    let endpoint = state
        .config
        .endpoint(&workspace, alias, operation)
        .ok_or_else(|| {
            Fault::new(
                Code::PermissionDenied,
                "endpoint is not allowed for this workspace and operation",
            )
        })?;
    if body.len() > endpoint.maximum_body_bytes {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "request exceeds the endpoint byte limit",
        ));
    }
    Ok(PreparedRequest {
        endpoint,
        workspace,
        idempotency_key,
        request_digest: hash_bytes(body).to_string(),
    })
}

fn verify_governed_endpoint(
    endpoint: &crate::EndpointConfig,
    governed: &ResolvedEndpoint,
    operation: GatewayOperation,
) -> FaultResult<()> {
    let maximum_body_bytes = u64::try_from(endpoint.maximum_body_bytes)
        .map_err(|_| Fault::new(Code::OutOfRange, "endpoint body limit exceeds u64"))?;
    if governed.policy_epoch != endpoint.policy_epoch
        || governed.endpoint.name != endpoint.alias
        || governed.endpoint.route != endpoint.route
        || !governed.endpoint.operations.contains(&operation)
        || governed.endpoint.connection_ref != endpoint.connection_ref
        || !governed.endpoint.guardrail_refs.is_empty()
        || governed.endpoint.maximum_body_bytes != maximum_body_bytes
        || governed.subject_maximum_request != endpoint.reservation
        || governed.endpoint.pricing_version != endpoint.pricing_version
        || governed.endpoint.request_micros != endpoint.request_micros
        || governed.endpoint.input_micros_per_million != endpoint.input_micros_per_million
        || governed.endpoint.output_micros_per_million != endpoint.output_micros_per_million
        || !governed.endpoint.metadata_only_tracing
        || governed.endpoint.usage_tracking
    {
        return Err(Fault::new(
            Code::FailedPrecondition,
            "local provider connection does not match the governed endpoint policy",
        ));
    }
    Ok(())
}

async fn terminalize(
    state: &AppState,
    subject: &str,
    endpoint: &crate::EndpointConfig,
    dispatched: &ReservationDecision,
    request_digest: &str,
    provider: &ProviderResult,
) -> FaultResult<()> {
    if (200..300).contains(&provider.status)
        && let Some(usage) = &provider.usage
        && let Ok(actual) = endpoint.actual_quota(usage.input_tokens, usage.output_tokens)
        && state
            .control
            .commit(subject, dispatched, request_digest, &actual)
            .await
            .is_ok()
    {
        state.metrics.committed();
        return Ok(());
    }
    let pending = state
        .control
        .mark_reconciliation_pending(subject, dispatched, request_digest)
        .await
        .map_err(|_| {
            Fault::new(
                Code::Unavailable,
                "provider outcome is ambiguous and reconciliation could not be recorded",
            )
        })?;
    state.metrics.reconciliation_pending();
    state
        .control
        .reconcile(subject, &pending, request_digest)
        .await
        .map_err(|_| {
            Fault::new(
                Code::Unavailable,
                "provider outcome is pending durable reconciliation",
            )
        })?;
    state.metrics.reconciled();
    Ok(())
}

async fn mark_pending(
    state: &AppState,
    subject: &str,
    dispatched: &ReservationDecision,
    digest: &str,
) {
    if state
        .control
        .mark_reconciliation_pending(subject, dispatched, digest)
        .await
        .is_ok()
    {
        state.metrics.reconciliation_pending();
    }
}

fn provider_response(provider: ProviderResult) -> FaultResult<Response> {
    let status = StatusCode::from_u16(provider.status)
        .map_err(|_| Fault::data_loss("provider returned an invalid HTTP status"))?;
    let mut response = (status, provider.body).into_response();
    response.headers_mut().insert(
        header::CONTENT_TYPE,
        HeaderValue::from_str(&provider.content_type)
            .unwrap_or_else(|_| HeaderValue::from_static("application/json")),
    );
    response
        .headers_mut()
        .insert(header::CACHE_CONTROL, HeaderValue::from_static("no-store"));
    for (name, value) in provider.response_headers {
        if let (Ok(name), Ok(value)) = (name.parse::<HeaderName>(), HeaderValue::from_str(&value)) {
            response.headers_mut().insert(name, value);
        }
    }
    Ok(response)
}

fn bearer_token(headers: &HeaderMap) -> FaultResult<&str> {
    let value = single_header(headers, header::AUTHORIZATION.as_str(), 16 << 10)?;
    value
        .strip_prefix("Bearer ")
        .filter(|token| !token.is_empty() && !token.bytes().any(|byte| byte.is_ascii_whitespace()))
        .ok_or_else(|| Fault::new(Code::Unauthenticated, "Bearer Google ID token is required"))
}

fn single_header<'a>(headers: &'a HeaderMap, name: &str, maximum: usize) -> FaultResult<&'a str> {
    let mut values = headers.get_all(name).iter();
    let value = values
        .next()
        .ok_or_else(|| Fault::invalid_argument(format!("required {name} header is missing")))?;
    if values.next().is_some() {
        return Err(Fault::invalid_argument(format!(
            "{name} header must not be repeated"
        )));
    }
    let value = value
        .to_str()
        .map_err(|_| Fault::invalid_argument(format!("{name} header is invalid")))?;
    if value.is_empty()
        || value.len() > maximum
        || value.bytes().any(|byte| byte < 0x20 || byte == 0x7f)
    {
        return Err(Fault::invalid_argument(format!("{name} header is invalid")));
    }
    Ok(value)
}

/// Renders the canonical HTTP status for a fault code.
///
/// The table is `mindclade_faults::status`, which mirrors `libs/go/httpx`. This
/// proxy used to keep its own `match`, which rendered `AlreadyExists` and
/// `OutOfRange` as 500 while the adjacent arms gave `Conflict` a 409 and
/// `InvalidArgument` a 400, and gave `FailedPrecondition` a 409 rather than the
/// 412 the Go edge answers.
///
/// `from_u16` is fallible only outside 100..1000 and the canonical table yields
/// nothing there — `mindclade_faults::status` asserts every code renders in
/// 400..600 — so the fallback is unreachable rather than a policy.
fn http_status(code: Code) -> StatusCode {
    StatusCode::from_u16(status::http_status(code)).unwrap_or(StatusCode::INTERNAL_SERVER_ERROR)
}

fn fault_response(error: &Fault) -> Response {
    let status = http_status(error.code());
    let mut response = (
        status,
        axum::Json(json!({
            "error": {"message": error.message(), "type": error.code().as_str()}
        })),
    )
        .into_response();
    response
        .headers_mut()
        .insert(header::CACHE_CONTROL, HeaderValue::from_static("no-store"));
    response
}

#[cfg(test)]
mod tests {
    use super::{Code, Fault, StatusCode, fault_response, status};

    /// Every fault code renders the canonical HTTP status.
    ///
    /// The numbers are restated here rather than read back from
    /// `mindclade_faults::status`. Reading them back would assert only that
    /// this edge calls the shared function, and the defect being closed is two
    /// edges that each called nothing shared: these values are the
    /// client-visible contract and a change to any one of them has to break a
    /// test that names it. This table is byte-for-byte the one in
    /// `services/runtime_gateway/src/network.rs`, which is the point.
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

    /// The regression on this side. `AlreadyExists` and `OutOfRange` rendered
    /// as 500 while the arms beside them gave `Conflict` a 409 and
    /// `InvalidArgument` a 400 — the same fault class, split across an
    /// availability signal and a client error by nothing but arm order.
    #[test]
    fn request_shaped_faults_are_not_reported_as_server_failures() {
        for code in [Code::AlreadyExists, Code::OutOfRange, Code::Cancelled] {
            let rendered = fault_response(&Fault::new(code, "rendered")).status();
            assert_ne!(
                rendered,
                StatusCode::INTERNAL_SERVER_ERROR,
                "{code} still renders as a server failure"
            );
        }
    }

    /// 412, not 409. A precondition failure is separately actionable from a
    /// state conflict, and `httpx.StatusFromCode` has always said so.
    #[test]
    fn a_failed_precondition_is_distinguishable_from_a_conflict() {
        let precondition = fault_response(&Fault::new(Code::FailedPrecondition, "rendered"));
        let conflict = fault_response(&Fault::new(Code::Conflict, "rendered"));
        assert_eq!(precondition.status(), StatusCode::PRECONDITION_FAILED);
        assert_eq!(conflict.status(), StatusCode::CONFLICT);
    }
}
