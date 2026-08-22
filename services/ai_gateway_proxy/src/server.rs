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
use mindclade_faults::{Code, Fault, FaultResult};
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

fn fault_response(error: &Fault) -> Response {
    let status = match error.code() {
        Code::InvalidArgument => StatusCode::BAD_REQUEST,
        Code::Unauthenticated => StatusCode::UNAUTHORIZED,
        Code::PermissionDenied => StatusCode::FORBIDDEN,
        Code::NotFound => StatusCode::NOT_FOUND,
        Code::Conflict | Code::Aborted | Code::FailedPrecondition => StatusCode::CONFLICT,
        Code::ResourceExhausted => StatusCode::TOO_MANY_REQUESTS,
        Code::Unimplemented => StatusCode::NOT_IMPLEMENTED,
        Code::DeadlineExceeded => StatusCode::GATEWAY_TIMEOUT,
        Code::Unavailable => StatusCode::SERVICE_UNAVAILABLE,
        _ => StatusCode::INTERNAL_SERVER_ERROR,
    };
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
