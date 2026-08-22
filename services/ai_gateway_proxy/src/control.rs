// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::{
    config::Secret,
    model::{
        GatewayOperation, LifecycleRequest, Problem, Quota, ReservationDecision, ReserveRequest,
        ResolvedEndpoint, Route,
    },
};
use futures_util::StreamExt;
use mindclade_faults::{Code, Fault, FaultResult};
use reqwest::{Client, StatusCode, Url};
use std::{future::Future, pin::Pin, time::Duration};

const MAXIMUM_CONTROL_RESPONSE_BYTES: usize = 1 << 20;
const HEADER_DELEGATED_SUBJECT: &str = "x-mindclade-delegated-subject";

pub trait ControlPlane: Send + Sync + std::fmt::Debug {
    fn resolve<'a>(
        &'a self,
        subject: &'a str,
        workspace: &'a str,
        endpoint: &'a str,
        operation: GatewayOperation,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ResolvedEndpoint>> + Send + 'a>>;
    fn reserve<'a>(
        &'a self,
        input: ReserveInput<'a>,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>>;
    fn dispatch<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        request_digest: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>>;
    fn mark_reconciliation_pending<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        request_digest: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>>;
    fn commit<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        request_digest: &'a str,
        actual: &'a Quota,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>>;
    fn reconcile<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        request_digest: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>>;
    fn release<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        request_digest: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>>;
}

#[derive(Clone, Copy, Debug)]
pub struct ReserveInput<'a> {
    pub subject: &'a str,
    pub request_digest: &'a str,
    pub idempotency_key: &'a str,
    pub workspace: &'a str,
    pub route: &'a Route,
    pub policy_epoch: u64,
    pub requested: &'a Quota,
    pub ttl_seconds: u32,
}

#[derive(Clone, Debug)]
pub struct HttpControlPlane {
    client: Client,
    base_url: Url,
    token: Secret,
}

impl HttpControlPlane {
    pub fn new(base_url: Url, token: Secret, timeout: Duration) -> FaultResult<Self> {
        let client = Client::builder()
            .connect_timeout(timeout)
            .timeout(timeout)
            .redirect(reqwest::redirect::Policy::none())
            .build()
            .map_err(|error| {
                Fault::new(
                    Code::FailedPrecondition,
                    "control-plane HTTP client configuration failed",
                )
                .with_source(error)
            })?;
        Ok(Self {
            client,
            base_url,
            token,
        })
    }

    fn reservations_url(&self) -> FaultResult<Url> {
        self.base_url
            .join("/v1/ai-gateway/reservations")
            .map_err(|error| {
                Fault::internal("control-plane reservation URL construction failed")
                    .with_source(error)
            })
    }

    fn lifecycle_url(&self, id: &str, action: &str) -> FaultResult<Url> {
        if id.is_empty() || id.contains('/') {
            return Err(Fault::data_loss(
                "control-plane returned an invalid reservation ID",
            ));
        }
        self.base_url
            .join(&format!("/v1/ai-gateway/reservations/{id}/{action}"))
            .map_err(|error| {
                Fault::internal("control-plane lifecycle URL construction failed")
                    .with_source(error)
            })
    }

    fn endpoint_url(
        &self,
        workspace: &str,
        endpoint: &str,
        operation: GatewayOperation,
    ) -> FaultResult<Url> {
        if workspace.is_empty()
            || workspace.contains('/')
            || endpoint.is_empty()
            || endpoint.contains('/')
        {
            return Err(Fault::invalid_argument(
                "endpoint resolution path is invalid",
            ));
        }
        let mut url = self
            .base_url
            .join(&format!(
                "/v1/ai-gateway/workspaces/{workspace}/endpoints/{endpoint}"
            ))
            .map_err(|error| {
                Fault::internal("control-plane endpoint URL construction failed").with_source(error)
            })?;
        url.query_pairs_mut()
            .append_pair("operation", operation.policy_name());
        Ok(url)
    }

    async fn decode(response: reqwest::Response) -> FaultResult<ReservationDecision> {
        let status = response.status();
        let body = collect_bounded(response, MAXIMUM_CONTROL_RESPONSE_BYTES).await?;
        if !status.is_success() {
            let problem = serde_json::from_slice::<Problem>(&body).unwrap_or(Problem {
                reason: "control_plane_rejected".to_owned(),
                detail: String::new(),
            });
            return Err(Fault::new(
                status_code(status),
                "control-plane admission rejected the request",
            )
            .with_context("reason", bounded_reason(&problem.reason))
            .with_context("detail_present", (!problem.detail.is_empty()).to_string()));
        }
        serde_json::from_slice(&body).map_err(|error| {
            Fault::data_loss("control-plane admission response is invalid").with_source(error)
        })
    }

    async fn lifecycle(
        &self,
        subject: &str,
        reservation: &ReservationDecision,
        request_digest: &str,
        action: &str,
        actual: Option<&Quota>,
    ) -> FaultResult<ReservationDecision> {
        let response = self
            .client
            .post(self.lifecycle_url(&reservation.reservation.id, action)?)
            .bearer_auth(self.token.expose())
            .header(HEADER_DELEGATED_SUBJECT, subject)
            .header(
                "If-Match",
                format!("\"{}\"", reservation.reservation.resource_version),
            )
            .json(&LifecycleRequest {
                request_digest,
                actual,
            })
            .send()
            .await
            .map_err(|error| {
                Fault::new(Code::Unavailable, "control-plane lifecycle request failed")
                    .with_source(error)
            })?;
        Self::decode(response).await
    }
}

impl ControlPlane for HttpControlPlane {
    fn resolve<'a>(
        &'a self,
        subject: &'a str,
        workspace: &'a str,
        endpoint: &'a str,
        operation: GatewayOperation,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ResolvedEndpoint>> + Send + 'a>> {
        Box::pin(async move {
            let response = self
                .client
                .get(self.endpoint_url(workspace, endpoint, operation)?)
                .bearer_auth(self.token.expose())
                .header(HEADER_DELEGATED_SUBJECT, subject)
                .send()
                .await
                .map_err(|error| {
                    Fault::new(
                        Code::Unavailable,
                        "control-plane endpoint resolution failed",
                    )
                    .with_source(error)
                })?;
            let status = response.status();
            let body = collect_bounded(response, MAXIMUM_CONTROL_RESPONSE_BYTES).await?;
            if !status.is_success() {
                let problem = serde_json::from_slice::<Problem>(&body).unwrap_or(Problem {
                    reason: "control_plane_rejected".to_owned(),
                    detail: String::new(),
                });
                return Err(Fault::new(
                    status_code(status),
                    "control-plane endpoint resolution was rejected",
                )
                .with_context("reason", bounded_reason(&problem.reason))
                .with_context("detail_present", (!problem.detail.is_empty()).to_string()));
            }
            serde_json::from_slice(&body).map_err(|error| {
                Fault::data_loss("control-plane endpoint response is invalid").with_source(error)
            })
        })
    }

    fn reserve<'a>(
        &'a self,
        input: ReserveInput<'a>,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>> {
        Box::pin(async move {
            let response = self
                .client
                .post(self.reservations_url()?)
                .bearer_auth(self.token.expose())
                .header(HEADER_DELEGATED_SUBJECT, input.subject)
                .header("Idempotency-Key", input.idempotency_key)
                .json(&ReserveRequest {
                    request_digest: input.request_digest,
                    workspace: input.workspace,
                    route: input.route,
                    policy_epoch: input.policy_epoch,
                    requested: input.requested,
                    ttl_seconds: input.ttl_seconds,
                })
                .send()
                .await
                .map_err(|error| {
                    Fault::new(
                        Code::Unavailable,
                        "control-plane reservation request failed",
                    )
                    .with_source(error)
                })?;
            Self::decode(response).await
        })
    }

    fn dispatch<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        request_digest: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>> {
        Box::pin(self.lifecycle(subject, reservation, request_digest, "dispatch", None))
    }

    fn mark_reconciliation_pending<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        request_digest: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>> {
        Box::pin(self.lifecycle(
            subject,
            reservation,
            request_digest,
            "reconciliation-pending",
            None,
        ))
    }

    fn commit<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        request_digest: &'a str,
        actual: &'a Quota,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>> {
        Box::pin(self.lifecycle(subject, reservation, request_digest, "commit", Some(actual)))
    }

    fn reconcile<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        request_digest: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>> {
        Box::pin(self.lifecycle(subject, reservation, request_digest, "reconcile", None))
    }

    fn release<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        request_digest: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>> {
        Box::pin(self.lifecycle(subject, reservation, request_digest, "release", None))
    }
}

async fn collect_bounded(response: reqwest::Response, maximum: usize) -> FaultResult<Vec<u8>> {
    if response
        .content_length()
        .is_some_and(|length| length > maximum as u64)
    {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "control-plane response exceeds the byte limit",
        ));
    }
    let mut stream = response.bytes_stream();
    let mut body = Vec::new();
    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|error| {
            Fault::new(Code::Unavailable, "control-plane response stream failed").with_source(error)
        })?;
        let next = body
            .len()
            .checked_add(chunk.len())
            .ok_or_else(|| Fault::new(Code::OutOfRange, "control-plane response size overflow"))?;
        if next > maximum {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "control-plane response exceeds the byte limit",
            ));
        }
        body.extend_from_slice(&chunk);
    }
    Ok(body)
}

fn status_code(status: StatusCode) -> Code {
    match status.as_u16() {
        400 => Code::InvalidArgument,
        401 => Code::Unauthenticated,
        403 => Code::PermissionDenied,
        404 => Code::NotFound,
        409 => Code::Conflict,
        412 => Code::FailedPrecondition,
        429 => Code::ResourceExhausted,
        500..=599 => Code::Unavailable,
        _ => Code::Internal,
    }
}

fn bounded_reason(value: &str) -> String {
    value
        .chars()
        .take(128)
        .filter(|character| character.is_ascii_alphanumeric() || matches!(character, '_' | '-'))
        .collect()
}
