// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::{EndpointConfig, GatewayOperation, ProviderResult, Quota};
use bytes::Bytes;
use futures_util::StreamExt;
use mindclade_faults::{Code, Fault, FaultResult};
use reqwest::{Certificate, Client, Url};
use serde_json::Value;
use std::{
    collections::BTreeMap, fs::File, future::Future, io::Read, path::Path, pin::Pin, time::Duration,
};

const MAX_CA_BUNDLE_BYTES: u64 = 256 * 1024;
const MAX_CA_CERTIFICATES: usize = 32;

pub trait Provider: Send + Sync + std::fmt::Debug {
    fn invoke<'a>(
        &'a self,
        endpoint: &'a EndpointConfig,
        operation: GatewayOperation,
        body: Bytes,
        maximum_response_bytes: usize,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ProviderResult>> + Send + 'a>>;
}

#[derive(Clone, Debug)]
pub struct HttpProvider {
    client: Client,
}

impl HttpProvider {
    pub fn new(
        timeout: Duration,
        egress_proxy_url: &Url,
        ca_bundle_path: &Path,
    ) -> FaultResult<Self> {
        let proxy = reqwest::Proxy::all(egress_proxy_url.as_str()).map_err(|error| {
            Fault::new(
                Code::FailedPrecondition,
                "provider egress proxy configuration failed",
            )
            .with_source(error)
        })?;
        let certificates = load_ca_bundle(ca_bundle_path)?;
        let client = Client::builder()
            .connect_timeout(Duration::from_secs(10).min(timeout))
            .timeout(timeout)
            .redirect(reqwest::redirect::Policy::none())
            .proxy(proxy)
            .tls_certs_only(certificates)
            .pool_max_idle_per_host(64)
            .build()
            .map_err(|error| {
                Fault::new(
                    Code::FailedPrecondition,
                    "provider HTTP client configuration failed",
                )
                .with_source(error)
            })?;
        Ok(Self { client })
    }
}

fn load_ca_bundle(path: &Path) -> FaultResult<Vec<Certificate>> {
    let file = File::open(path).map_err(|error| {
        Fault::new(
            Code::FailedPrecondition,
            "egress CA bundle cannot be opened",
        )
        .with_source(error)
    })?;
    let mut bytes = Vec::new();
    file.take(MAX_CA_BUNDLE_BYTES + 1)
        .read_to_end(&mut bytes)
        .map_err(|error| {
            Fault::new(Code::FailedPrecondition, "egress CA bundle cannot be read")
                .with_source(error)
        })?;
    if bytes.is_empty() || bytes.len() as u64 > MAX_CA_BUNDLE_BYTES {
        return Err(Fault::new(
            Code::FailedPrecondition,
            "egress CA bundle size is invalid",
        ));
    }
    let certificates = Certificate::from_pem_bundle(&bytes).map_err(|error| {
        Fault::new(Code::FailedPrecondition, "egress CA bundle is invalid").with_source(error)
    })?;
    if certificates.is_empty() || certificates.len() > MAX_CA_CERTIFICATES {
        return Err(Fault::new(
            Code::FailedPrecondition,
            "egress CA certificate count is invalid",
        ));
    }
    Ok(certificates)
}

impl Provider for HttpProvider {
    fn invoke<'a>(
        &'a self,
        endpoint: &'a EndpointConfig,
        operation: GatewayOperation,
        body: Bytes,
        maximum_response_bytes: usize,
    ) -> Pin<Box<dyn Future<Output = FaultResult<ProviderResult>> + Send + 'a>> {
        Box::pin(async move {
            let mut document: Value = serde_json::from_slice(&body).map_err(|error| {
                Fault::invalid_argument("OpenAI-compatible request body is invalid")
                    .with_source(error)
            })?;
            let object = document.as_object_mut().ok_or_else(|| {
                Fault::invalid_argument("OpenAI-compatible request body must be an object")
            })?;
            if object
                .get("stream")
                .and_then(Value::as_bool)
                .unwrap_or(false)
            {
                return Err(Fault::new(
                    Code::Unimplemented,
                    "streaming requests are not enabled until streaming reconciliation qualification passes",
                ));
            }
            object.insert(
                "model".to_owned(),
                Value::String(endpoint.route.model.clone()),
            );
            let outbound = serde_json::to_vec(&document).map_err(|error| {
                Fault::internal("provider request encoding failed").with_source(error)
            })?;
            if outbound.len() > endpoint.maximum_body_bytes {
                return Err(Fault::new(
                    Code::ResourceExhausted,
                    "provider request exceeds the endpoint byte limit",
                ));
            }
            let url = endpoint
                .provider_base_url
                .join(operation.path())
                .map_err(|error| {
                    Fault::internal("provider URL construction failed").with_source(error)
                })?;
            if url.scheme() != endpoint.provider_base_url.scheme()
                || url.host_str() != endpoint.provider_base_url.host_str()
                || url.port_or_known_default() != endpoint.provider_base_url.port_or_known_default()
            {
                return Err(Fault::new(
                    Code::PermissionDenied,
                    "provider URL escaped the configured origin",
                ));
            }
            let response = self
                .client
                .post(url)
                .bearer_auth(endpoint.provider_token.expose())
                .header("Content-Type", "application/json")
                .header("Accept", "application/json")
                .body(outbound)
                .send()
                .await
                .map_err(|error| {
                    Fault::new(Code::Unavailable, "provider request failed after dispatch")
                        .with_source(error)
                })?;
            let status = response.status().as_u16();
            let content_type = response
                .headers()
                .get(reqwest::header::CONTENT_TYPE)
                .and_then(|value| value.to_str().ok())
                .filter(|value| value.starts_with("application/json"))
                .unwrap_or("application/json")
                .to_owned();
            let mut response_headers = BTreeMap::new();
            for name in ["x-request-id", "openai-processing-ms"] {
                if let Some(value) = response
                    .headers()
                    .get(name)
                    .and_then(|value| value.to_str().ok())
                    && value.len() <= 256
                    && value.bytes().all(|byte| byte >= 0x20 && byte != 0x7f)
                {
                    response_headers.insert(name.to_owned(), value.to_owned());
                }
            }
            let body = collect_bounded(response, maximum_response_bytes).await?;
            let usage = if (200..300).contains(&status) {
                parse_usage(&body)?
            } else {
                None
            };
            Ok(ProviderResult {
                status,
                content_type,
                body,
                usage,
                response_headers,
            })
        })
    }
}

async fn collect_bounded(response: reqwest::Response, maximum: usize) -> FaultResult<Bytes> {
    if response
        .content_length()
        .is_some_and(|length| length > maximum as u64)
    {
        return Err(Fault::new(
            Code::ResourceExhausted,
            "provider response exceeds the byte limit",
        ));
    }
    let mut stream = response.bytes_stream();
    let mut body = Vec::new();
    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(|error| {
            Fault::new(
                Code::Unavailable,
                "provider response stream failed after dispatch",
            )
            .with_source(error)
        })?;
        let next = body
            .len()
            .checked_add(chunk.len())
            .ok_or_else(|| Fault::new(Code::OutOfRange, "provider response size overflow"))?;
        if next > maximum {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "provider response exceeds the byte limit",
            ));
        }
        body.extend_from_slice(&chunk);
    }
    Ok(Bytes::from(body))
}

fn parse_usage(body: &[u8]) -> FaultResult<Option<Quota>> {
    let document: Value = serde_json::from_slice(body).map_err(|error| {
        Fault::data_loss("successful provider response is not valid JSON").with_source(error)
    })?;
    let Some(usage) = document.get("usage").and_then(Value::as_object) else {
        return Ok(None);
    };
    let input_tokens = unsigned(usage.get("input_tokens"))
        .or_else(|| unsigned(usage.get("prompt_tokens")))
        .unwrap_or(0);
    let output_tokens = unsigned(usage.get("output_tokens"))
        .or_else(|| unsigned(usage.get("completion_tokens")))
        .unwrap_or(0);
    if input_tokens == 0
        && output_tokens == 0
        && unsigned(usage.get("total_tokens")).unwrap_or(0) > 0
    {
        return Ok(None);
    }
    Ok(Some(Quota {
        requests: 1,
        input_tokens,
        output_tokens,
        cost_micros: 0,
    }))
}

fn unsigned(value: Option<&Value>) -> Option<u64> {
    value.and_then(Value::as_u64)
}
