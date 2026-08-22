// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use bytes::Bytes;
use serde::{Deserialize, Serialize};
use std::collections::BTreeMap;

#[derive(Clone, Copy, Debug, Deserialize, Eq, Ord, PartialEq, PartialOrd, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum GatewayOperation {
    ChatCompletions,
    Responses,
    Embeddings,
}

impl GatewayOperation {
    #[must_use]
    pub const fn path(self) -> &'static str {
        match self {
            Self::ChatCompletions => "/v1/chat/completions",
            Self::Responses => "/v1/responses",
            Self::Embeddings => "/v1/embeddings",
        }
    }

    #[must_use]
    pub const fn policy_name(self) -> &'static str {
        match self {
            Self::ChatCompletions => "chat.completions",
            Self::Responses => "responses",
            Self::Embeddings => "embeddings",
        }
    }
}

#[derive(Clone, Debug, Default, Deserialize, Eq, PartialEq, Serialize)]
pub struct Quota {
    #[serde(default)]
    pub requests: u64,
    #[serde(default)]
    pub input_tokens: u64,
    #[serde(default)]
    pub output_tokens: u64,
    #[serde(default)]
    pub cost_micros: u64,
}

impl Quota {
    #[must_use]
    pub fn fits(&self, limit: &Self) -> bool {
        self.requests <= limit.requests
            && self.input_tokens <= limit.input_tokens
            && self.output_tokens <= limit.output_tokens
            && self.cost_micros <= limit.cost_micros
    }
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Route {
    pub endpoint: String,
    pub provider: String,
    pub model: String,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct EndpointPolicy {
    pub name: String,
    pub route: Route,
    pub operations: Vec<GatewayOperation>,
    pub connection_ref: String,
    #[serde(default)]
    pub guardrail_refs: Vec<String>,
    pub maximum_body_bytes: u64,
    pub maximum_request: Quota,
    pub pricing_version: u64,
    pub request_micros: u64,
    pub input_micros_per_million: u64,
    pub output_micros_per_million: u64,
    pub metadata_only_tracing: bool,
    pub usage_tracking: bool,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct ResolvedEndpoint {
    pub policy_epoch: u64,
    pub bundle_version: String,
    pub endpoint: EndpointPolicy,
    pub subject_maximum_request: Quota,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Reservation {
    pub id: String,
    pub workspace: String,
    pub route: Route,
    pub policy_epoch: u64,
    pub reserved: Quota,
    pub state: String,
    pub resource_version: String,
}

#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct ReservationDecision {
    pub reservation: Reservation,
    pub replayed: bool,
}

#[derive(Clone, Debug, Serialize)]
pub struct ReserveRequest<'a> {
    pub request_digest: &'a str,
    pub workspace: &'a str,
    pub route: &'a Route,
    pub policy_epoch: u64,
    pub requested: &'a Quota,
    pub ttl_seconds: u32,
}

#[derive(Clone, Debug, Serialize)]
pub struct LifecycleRequest<'a> {
    pub request_digest: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub actual: Option<&'a Quota>,
}

#[derive(Clone, Debug)]
pub struct ProviderResult {
    pub status: u16,
    pub content_type: String,
    pub body: Bytes,
    pub usage: Option<Quota>,
    pub response_headers: BTreeMap<String, String>,
}

#[derive(Debug, Deserialize)]
pub struct Problem {
    #[serde(default)]
    pub reason: String,
    #[serde(default)]
    pub detail: String,
}
