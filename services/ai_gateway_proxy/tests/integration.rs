// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

#![forbid(unsafe_code)]

use axum::{
    body::{Body, Bytes, to_bytes},
    http::{Request, StatusCode},
};
use mindclade_ai_gateway_proxy::{
    AppState, ControlPlane, EndpointConfig, EndpointPolicy, GatewayOperation, Identity,
    IdentityVerifier, Provider, ProviderResult, ProxyConfig, Quota, ReservationDecision,
    ResolvedEndpoint,
    config::Secret,
    control::ReserveInput,
    model::{Reservation, Route},
    router,
};
use mindclade_faults::{Code, Fault, FaultResult};
use reqwest::Url;
use std::{
    collections::{BTreeMap, BTreeSet},
    future::Future,
    net::{IpAddr, Ipv4Addr, SocketAddr},
    pin::Pin,
    sync::{Arc, Mutex},
    time::Duration,
};
use tower::ServiceExt;

#[derive(Clone, Debug)]
struct TestVerifier;

impl IdentityVerifier for TestVerifier {
    fn verify<'a>(
        &'a self,
        token: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<Identity>> + Send + 'a>> {
        Box::pin(async move {
            if token != "valid-token" {
                return Err(Fault::new(Code::Unauthenticated, "test identity rejected"));
            }
            Ok(Identity {
                subject: "service-account:caller".to_owned(),
                email: None,
            })
        })
    }
}

#[derive(Clone, Debug)]
struct TestControl {
    actions: Arc<Mutex<Vec<String>>>,
}

impl TestControl {
    fn record(&self, action: &str) {
        self.actions
            .lock()
            .expect("action lock")
            .push(action.to_owned());
    }

    fn transitioned(
        reservation: &ReservationDecision,
        state: &str,
        version: &str,
    ) -> ReservationDecision {
        let mut next = reservation.clone();
        state.clone_into(&mut next.reservation.state);
        version.clone_into(&mut next.reservation.resource_version);
        next.replayed = false;
        next
    }
}

impl ControlPlane for TestControl {
    fn resolve<'a>(
        &'a self,
        subject: &'a str,
        workspace: &'a str,
        endpoint: &'a str,
        operation: GatewayOperation,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ResolvedEndpoint>> + Send + 'a>> {
        Box::pin(async move {
            self.record("resolve");
            assert_eq!(
                (subject, workspace, endpoint, operation),
                (
                    "google-ef8168663494fc8a1b3267fb3a9f929b155bd6a9fce5a83735c2d80f3830197c",
                    "workspace-a",
                    "governed-chat",
                    GatewayOperation::ChatCompletions
                )
            );
            Ok(ResolvedEndpoint {
                policy_epoch: 7,
                bundle_version: "1-sha256:bundle".to_owned(),
                endpoint: EndpointPolicy {
                    name: "governed-chat".to_owned(),
                    route: Route {
                        endpoint: "governed-chat".to_owned(),
                        provider: "openai".to_owned(),
                        model: "gpt-qualified".to_owned(),
                    },
                    operations: vec![GatewayOperation::ChatCompletions],
                    connection_ref: "openai-primary".to_owned(),
                    guardrail_refs: Vec::new(),
                    maximum_body_bytes: 1 << 20,
                    maximum_request: Quota {
                        requests: 1,
                        input_tokens: 2_000,
                        output_tokens: 2_000,
                        cost_micros: 20_000,
                    },
                    pricing_version: 1,
                    request_micros: 5,
                    input_micros_per_million: 1_000_000,
                    output_micros_per_million: 2_000_000,
                    metadata_only_tracing: true,
                    usage_tracking: false,
                },
                subject_maximum_request: Quota {
                    requests: 1,
                    input_tokens: 1_000,
                    output_tokens: 1_000,
                    cost_micros: 10_000,
                },
            })
        })
    }

    fn reserve<'a>(
        &'a self,
        input: ReserveInput<'a>,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>> {
        Box::pin(async move {
            self.record("reserve");
            assert_eq!(
                input.subject,
                "google-ef8168663494fc8a1b3267fb3a9f929b155bd6a9fce5a83735c2d80f3830197c"
            );
            Ok(ReservationDecision {
                reservation: Reservation {
                    id: "01K4M8GF1P2X3Y4Z5A6B7C8D9E".to_owned(),
                    workspace: input.workspace.to_owned(),
                    route: input.route.clone(),
                    policy_epoch: input.policy_epoch,
                    reserved: input.requested.clone(),
                    state: "reserved".to_owned(),
                    resource_version: "1".to_owned(),
                },
                replayed: false,
            })
        })
    }

    fn dispatch<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        _: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>> {
        Box::pin(async move {
            assert_delegated_subject(subject);
            self.record("dispatch");
            Ok(Self::transitioned(reservation, "dispatched", "2"))
        })
    }

    fn mark_reconciliation_pending<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        _: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>> {
        Box::pin(async move {
            assert_delegated_subject(subject);
            self.record("pending");
            Ok(Self::transitioned(
                reservation,
                "reconciliation_pending",
                "3",
            ))
        })
    }

    fn commit<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        _: &'a str,
        actual: &'a Quota,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>> {
        Box::pin(async move {
            assert_delegated_subject(subject);
            assert_eq!(actual.cost_micros, 205);
            self.record("commit");
            Ok(Self::transitioned(reservation, "committed", "3"))
        })
    }

    fn reconcile<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        _: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>> {
        Box::pin(async move {
            assert_delegated_subject(subject);
            self.record("reconcile");
            Ok(Self::transitioned(reservation, "committed", "4"))
        })
    }

    fn release<'a>(
        &'a self,
        subject: &'a str,
        reservation: &'a ReservationDecision,
        _: &'a str,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ReservationDecision>> + Send + 'a>> {
        Box::pin(async move {
            assert_delegated_subject(subject);
            self.record("release");
            Ok(Self::transitioned(reservation, "released", "2"))
        })
    }
}

fn assert_delegated_subject(subject: &str) {
    assert_eq!(
        subject,
        "google-ef8168663494fc8a1b3267fb3a9f929b155bd6a9fce5a83735c2d80f3830197c"
    );
}

#[derive(Clone, Copy, Debug)]
enum ProviderMode {
    Success,
    Rejected,
    AmbiguousFailure,
}

#[derive(Clone, Debug)]
struct TestProvider {
    mode: ProviderMode,
}

impl Provider for TestProvider {
    fn invoke<'a>(
        &'a self,
        _: &'a EndpointConfig,
        _: GatewayOperation,
        _: Bytes,
        _: usize,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ProviderResult>> + Send + 'a>> {
        Box::pin(async move {
            match self.mode {
                ProviderMode::Success => Ok(ProviderResult {
                    status: 200,
                    content_type: "application/json".to_owned(),
                    body: Bytes::from_static(br#"{"id":"result"}"#),
                    usage: Some(Quota {
                        requests: 1,
                        input_tokens: 100,
                        output_tokens: 50,
                        cost_micros: 0,
                    }),
                    response_headers: BTreeMap::new(),
                }),
                ProviderMode::Rejected => Ok(ProviderResult {
                    status: 429,
                    content_type: "application/json".to_owned(),
                    body: Bytes::from_static(br#"{"error":{"message":"provider capacity"}}"#),
                    usage: None,
                    response_headers: BTreeMap::new(),
                }),
                ProviderMode::AmbiguousFailure => Err(Fault::new(
                    Code::Unavailable,
                    "provider outcome is ambiguous",
                )),
            }
        })
    }
}

fn test_state(mode: ProviderMode) -> (AppState, Arc<Mutex<Vec<String>>>) {
    let route = Route {
        endpoint: "governed-chat".to_owned(),
        provider: "openai".to_owned(),
        model: "gpt-qualified".to_owned(),
    };
    let endpoint = EndpointConfig {
        workspace_id: "workspace-a".to_owned(),
        alias: "governed-chat".to_owned(),
        operations: BTreeSet::from([GatewayOperation::ChatCompletions]),
        route,
        connection_ref: "openai-primary".to_owned(),
        policy_epoch: 7,
        pricing_version: 1,
        provider_base_url: Url::parse("https://api.openai.com/").expect("provider URL"),
        provider_token: Secret::new("provider-secret".to_owned()),
        reservation: Quota {
            requests: 1,
            input_tokens: 1_000,
            output_tokens: 1_000,
            cost_micros: 10_000,
        },
        request_micros: 5,
        input_micros_per_million: 1_000_000,
        output_micros_per_million: 2_000_000,
        maximum_body_bytes: 1 << 20,
    };
    let mut endpoints = BTreeMap::new();
    endpoints.insert(
        (endpoint.workspace_id.clone(), endpoint.alias.clone()),
        endpoint,
    );
    let config = Arc::new(ProxyConfig {
        listen_address: SocketAddr::new(IpAddr::V4(Ipv4Addr::LOCALHOST), 0),
        control_base_url: Url::parse("https://control.invalid/").expect("control URL"),
        control_token: Secret::new("control-secret".to_owned()),
        client_audience: "gateway-audience".to_owned(),
        google_jwks_url: Url::parse("https://www.googleapis.com/oauth2/v3/certs")
            .expect("JWKS URL"),
        egress_proxy_url: Url::parse("https://secure-web-proxy.internal:443").expect("proxy URL"),
        egress_ca_bundle_path: "/var/run/secrets/mindclade/egress-ca/ca.crt".into(),
        endpoints,
        maximum_body_bytes: 1 << 20,
        maximum_response_bytes: 1 << 20,
        maximum_concurrency: 8,
        provider_timeout: Duration::from_secs(30),
        control_timeout: Duration::from_secs(5),
        reservation_ttl_seconds: 300,
    });
    let actions = Arc::new(Mutex::new(Vec::new()));
    let control = Arc::new(TestControl {
        actions: actions.clone(),
    });
    let state = AppState::new(
        config,
        control,
        Arc::new(TestProvider { mode }),
        Arc::new(TestVerifier),
    );
    state.mark_ready();
    (state, actions)
}

fn request(token: &str) -> Request<Body> {
    Request::builder()
        .method("POST")
        .uri("/v1/chat/completions")
        .header("authorization", format!("Bearer {token}"))
        .header("x-mindclade-workspace-id", "workspace-a")
        .header("idempotency-key", "request-0001")
        .header("content-type", "application/json")
        .body(Body::from(
            r#"{"model":"governed-chat","messages":[{"role":"user","content":"private payload"}]}"#,
        ))
        .expect("request")
}

#[tokio::test]
async fn successful_request_commits_measured_usage() {
    let (state, actions) = test_state(ProviderMode::Success);
    let response = router(state.clone())
        .oneshot(request("valid-token"))
        .await
        .expect("response");
    assert_eq!(response.status(), StatusCode::OK);
    assert_eq!(
        actions.lock().expect("actions").as_slice(),
        ["resolve", "reserve", "dispatch", "commit"]
    );
    assert_eq!(
        state.metrics.snapshot().get("ai_gateway.committed"),
        Some(&1)
    );
}

#[tokio::test]
async fn provider_rejection_is_durably_max_charged_before_forwarding() {
    let (state, actions) = test_state(ProviderMode::Rejected);
    let response = router(state.clone())
        .oneshot(request("valid-token"))
        .await
        .expect("response");
    assert_eq!(response.status(), StatusCode::TOO_MANY_REQUESTS);
    assert_eq!(
        actions.lock().expect("actions").as_slice(),
        ["resolve", "reserve", "dispatch", "pending", "reconcile"]
    );
    assert_eq!(
        state.metrics.snapshot().get("ai_gateway.reconciled"),
        Some(&1)
    );
}

#[tokio::test]
async fn uncertain_transport_is_left_durably_pending_without_payload_disclosure() {
    let (state, actions) = test_state(ProviderMode::AmbiguousFailure);
    let response = router(state)
        .oneshot(request("valid-token"))
        .await
        .expect("response");
    assert_eq!(response.status(), StatusCode::SERVICE_UNAVAILABLE);
    assert_eq!(
        actions.lock().expect("actions").as_slice(),
        ["resolve", "reserve", "dispatch", "pending"]
    );
    let body = to_bytes(response.into_body(), 1 << 20).await.expect("body");
    assert!(!String::from_utf8_lossy(&body).contains("private payload"));
}

#[tokio::test]
async fn invalid_identity_is_rejected_before_reservation() {
    let (state, actions) = test_state(ProviderMode::Success);
    let response = router(state)
        .oneshot(request("invalid-token"))
        .await
        .expect("response");
    assert_eq!(response.status(), StatusCode::UNAUTHORIZED);
    assert!(actions.lock().expect("actions").is_empty());
}
