// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::model::{GatewayOperation, Quota, Route};
use mindclade_faults::{Code, Fault, FaultResult};
use reqwest::Url;
use serde::Deserialize;
use std::{
    collections::{BTreeMap, BTreeSet},
    env, fmt,
    net::SocketAddr,
    path::PathBuf,
    time::Duration,
};

const MAX_ENDPOINTS: usize = 128;

#[derive(Clone, Default)]
pub struct Secret(String);

impl Secret {
    #[must_use]
    pub fn new(value: String) -> Self {
        Self(value)
    }

    #[must_use]
    pub fn expose(&self) -> &str {
        &self.0
    }
}

impl fmt::Debug for Secret {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("[REDACTED]")
    }
}

#[derive(Clone, Debug)]
pub struct EndpointConfig {
    pub workspace_id: String,
    pub alias: String,
    pub operations: BTreeSet<GatewayOperation>,
    pub route: Route,
    pub connection_ref: String,
    pub policy_epoch: u64,
    pub pricing_version: u64,
    pub provider_base_url: Url,
    pub provider_token: Secret,
    pub reservation: Quota,
    pub request_micros: u64,
    pub input_micros_per_million: u64,
    pub output_micros_per_million: u64,
    pub maximum_body_bytes: usize,
}

impl EndpointConfig {
    pub fn actual_quota(&self, input_tokens: u64, output_tokens: u64) -> FaultResult<Quota> {
        let input_cost = input_tokens
            .checked_mul(self.input_micros_per_million)
            .and_then(|value| value.checked_add(999_999))
            .map(|value| value / 1_000_000)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "input token cost overflow"))?;
        let output_cost = output_tokens
            .checked_mul(self.output_micros_per_million)
            .and_then(|value| value.checked_add(999_999))
            .map(|value| value / 1_000_000)
            .ok_or_else(|| Fault::new(Code::OutOfRange, "output token cost overflow"))?;
        let cost_micros = self
            .request_micros
            .checked_add(input_cost)
            .and_then(|value| value.checked_add(output_cost))
            .ok_or_else(|| Fault::new(Code::OutOfRange, "request cost overflow"))?;
        let actual = Quota {
            requests: 1,
            input_tokens,
            output_tokens,
            cost_micros,
        };
        if !actual.fits(&self.reservation) {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "provider usage exceeds the governed reservation",
            ));
        }
        Ok(actual)
    }
}

#[derive(Clone)]
pub struct ProxyConfig {
    pub listen_address: SocketAddr,
    pub control_base_url: Url,
    pub control_token: Secret,
    pub client_audience: String,
    pub google_jwks_url: Url,
    pub egress_proxy_url: Url,
    pub egress_ca_bundle_path: PathBuf,
    pub endpoints: BTreeMap<(String, String), EndpointConfig>,
    pub maximum_body_bytes: usize,
    pub maximum_response_bytes: usize,
    pub maximum_concurrency: usize,
    pub provider_timeout: Duration,
    pub control_timeout: Duration,
    pub reservation_ttl_seconds: u32,
}

impl fmt::Debug for ProxyConfig {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("ProxyConfig")
            .field("listen_address", &self.listen_address)
            .field("control_base_url", &self.control_base_url)
            .field("control_token", &self.control_token)
            .field("client_audience", &self.client_audience)
            .field("google_jwks_url", &self.google_jwks_url)
            .field("egress_proxy_url", &self.egress_proxy_url)
            .field("egress_ca_bundle_path", &self.egress_ca_bundle_path)
            .field("endpoint_count", &self.endpoints.len())
            .field("maximum_body_bytes", &self.maximum_body_bytes)
            .field("maximum_response_bytes", &self.maximum_response_bytes)
            .field("maximum_concurrency", &self.maximum_concurrency)
            .field("provider_timeout", &self.provider_timeout)
            .field("control_timeout", &self.control_timeout)
            .field("reservation_ttl_seconds", &self.reservation_ttl_seconds)
            .finish()
    }
}

#[derive(Debug, Deserialize)]
struct EndpointInput {
    workspace_id: String,
    alias: String,
    operations: BTreeSet<GatewayOperation>,
    route: Route,
    connection_ref: String,
    policy_epoch: u64,
    pricing_version: u64,
    provider_base_url: String,
    provider_token_env: String,
    reservation: Quota,
    request_micros: u64,
    input_micros_per_million: u64,
    output_micros_per_million: u64,
    maximum_body_bytes: usize,
}

impl ProxyConfig {
    pub fn from_env() -> FaultResult<Self> {
        let allow_insecure_control = env::var("MINDCLADE_AI_GATEWAY_ALLOW_INSECURE_CONTROL_HTTP")
            .is_ok_and(|value| value == "true");
        let listen_address = required("MINDCLADE_AI_GATEWAY_LISTEN_ADDRESS")?
            .parse()
            .map_err(|_| Fault::invalid_argument("AI Gateway listen address is invalid"))?;
        let control_base_url = parse_base_url(
            &required("MINDCLADE_AI_GATEWAY_CONTROL_URL")?,
            allow_insecure_control,
            "control-plane",
        )?;
        let client_audience = required("MINDCLADE_AI_GATEWAY_CLIENT_AUDIENCE")?;
        let google_jwks_url = parse_base_url(
            &env::var("MINDCLADE_AI_GATEWAY_GOOGLE_JWKS_URL")
                .unwrap_or_else(|_| "https://www.googleapis.com/oauth2/v3/certs".to_owned()),
            false,
            "Google JWKS",
        )?;
        let egress_proxy_url =
            parse_proxy_url(&required("MINDCLADE_AI_GATEWAY_EGRESS_PROXY_URL")?)?;
        let egress_ca_bundle_path =
            parse_secret_path(&required("MINDCLADE_AI_GATEWAY_EGRESS_CA_BUNDLE_PATH")?)?;
        let endpoints = endpoints_from_env()?;
        let config = Self {
            listen_address,
            control_base_url,
            control_token: Secret(required("MINDCLADE_AI_GATEWAY_CONTROL_TOKEN")?),
            client_audience,
            google_jwks_url,
            egress_proxy_url,
            egress_ca_bundle_path,
            endpoints,
            maximum_body_bytes: bounded_usize(
                "MINDCLADE_AI_GATEWAY_MAXIMUM_BODY_BYTES",
                16 * 1024 * 1024,
                16 * 1024 * 1024,
            )?,
            maximum_response_bytes: bounded_usize(
                "MINDCLADE_AI_GATEWAY_MAXIMUM_RESPONSE_BYTES",
                64 * 1024 * 1024,
                64 * 1024 * 1024,
            )?,
            maximum_concurrency: bounded_usize(
                "MINDCLADE_AI_GATEWAY_MAXIMUM_CONCURRENCY",
                256,
                16_384,
            )?,
            provider_timeout: Duration::from_secs(bounded_u64(
                "MINDCLADE_AI_GATEWAY_PROVIDER_TIMEOUT_SECONDS",
                120,
                900,
            )?),
            control_timeout: Duration::from_secs(bounded_u64(
                "MINDCLADE_AI_GATEWAY_CONTROL_TIMEOUT_SECONDS",
                5,
                30,
            )?),
            reservation_ttl_seconds: u32::try_from(bounded_u64(
                "MINDCLADE_AI_GATEWAY_RESERVATION_TTL_SECONDS",
                300,
                900,
            )?)
            .map_err(|_| Fault::new(Code::OutOfRange, "reservation TTL exceeds u32"))?,
        };
        for endpoint in config.endpoints.values() {
            if endpoint.maximum_body_bytes > config.maximum_body_bytes {
                return Err(Fault::invalid_argument(
                    "endpoint body limit exceeds service limit",
                ));
            }
        }
        Ok(config)
    }

    #[must_use]
    pub fn endpoint(
        &self,
        workspace: &str,
        alias: &str,
        operation: GatewayOperation,
    ) -> Option<&EndpointConfig> {
        self.endpoints
            .get(&(workspace.to_owned(), alias.to_owned()))
            .filter(|endpoint| endpoint.operations.contains(&operation))
    }
}

fn endpoints_from_env() -> FaultResult<BTreeMap<(String, String), EndpointConfig>> {
    let endpoint_inputs: Vec<EndpointInput> =
        serde_json::from_str(&required("MINDCLADE_AI_GATEWAY_ENDPOINTS")?)
            .map_err(|_| Fault::invalid_argument("AI Gateway endpoint configuration is invalid"))?;
    if endpoint_inputs.is_empty() || endpoint_inputs.len() > MAX_ENDPOINTS {
        return Err(Fault::invalid_argument(
            "AI Gateway endpoint count is invalid",
        ));
    }
    let mut endpoints = BTreeMap::new();
    for input in endpoint_inputs {
        validate_name(&input.workspace_id, "workspace")?;
        validate_name(&input.alias, "endpoint alias")?;
        validate_name(&input.route.endpoint, "route endpoint")?;
        validate_name(&input.route.provider, "route provider")?;
        validate_name(&input.route.model, "route model")?;
        validate_name(&input.connection_ref, "connection reference")?;
        if input.alias != input.route.endpoint
            || input.alias.contains('/')
            || input.operations.is_empty()
            || input.policy_epoch == 0
            || input.pricing_version == 0
            || input.reservation.requests != 1
            || input.maximum_body_bytes == 0
            || input.maximum_body_bytes > 16 * 1024 * 1024
            || input.provider_token_env.trim().is_empty()
        {
            return Err(Fault::invalid_argument(
                "AI Gateway endpoint policy is invalid",
            ));
        }
        let provider_token = required(input.provider_token_env.trim())?;
        let endpoint = EndpointConfig {
            workspace_id: input.workspace_id,
            alias: input.alias,
            operations: input.operations,
            route: input.route,
            connection_ref: input.connection_ref,
            policy_epoch: input.policy_epoch,
            pricing_version: input.pricing_version,
            provider_base_url: parse_base_url(&input.provider_base_url, false, "provider")?,
            provider_token: Secret(provider_token),
            reservation: input.reservation,
            request_micros: input.request_micros,
            input_micros_per_million: input.input_micros_per_million,
            output_micros_per_million: input.output_micros_per_million,
            maximum_body_bytes: input.maximum_body_bytes,
        };
        let key = (endpoint.workspace_id.clone(), endpoint.alias.clone());
        if endpoints.insert(key, endpoint).is_some() {
            return Err(Fault::invalid_argument("AI Gateway endpoint is duplicated"));
        }
    }
    Ok(endpoints)
}

fn parse_proxy_url(value: &str) -> FaultResult<Url> {
    let parsed =
        Url::parse(value).map_err(|_| Fault::invalid_argument("egress proxy URL is invalid"))?;
    if parsed.scheme() != "https"
        || parsed.host_str().is_none()
        || !parsed.username().is_empty()
        || parsed.password().is_some()
        || parsed.query().is_some()
        || parsed.fragment().is_some()
    {
        return Err(Fault::invalid_argument("egress proxy URL is not allowed"));
    }
    Ok(parsed)
}

fn parse_secret_path(value: &str) -> FaultResult<PathBuf> {
    let path = PathBuf::from(value);
    if !path.is_absolute()
        || value.len() > 1024
        || path.components().any(|component| {
            matches!(
                component,
                std::path::Component::ParentDir | std::path::Component::CurDir
            )
        })
    {
        return Err(Fault::invalid_argument(
            "egress CA bundle path is not allowed",
        ));
    }
    Ok(path)
}

fn required(name: &str) -> FaultResult<String> {
    env::var(name)
        .ok()
        .filter(|value| !value.trim().is_empty())
        .ok_or_else(|| {
            Fault::new(
                Code::FailedPrecondition,
                format!("required setting {name} is missing"),
            )
        })
}

fn parse_base_url(value: &str, allow_insecure: bool, label: &str) -> FaultResult<Url> {
    let parsed = Url::parse(value)
        .map_err(|_| Fault::invalid_argument(format!("{label} URL is invalid")))?;
    let secure = parsed.scheme() == "https" || allow_insecure && parsed.scheme() == "http";
    if !secure
        || parsed.host_str().is_none()
        || !parsed.username().is_empty()
        || parsed.password().is_some()
        || parsed.query().is_some()
        || parsed.fragment().is_some()
    {
        return Err(Fault::invalid_argument(format!(
            "{label} URL is not an allowed base URL"
        )));
    }
    Ok(parsed)
}

fn bounded_usize(name: &str, default: usize, maximum: usize) -> FaultResult<usize> {
    let value = env::var(name).ok().map_or(Ok(default), |raw| {
        raw.parse()
            .map_err(|_| Fault::invalid_argument(format!("{name} is invalid")))
    })?;
    if value == 0 || value > maximum {
        return Err(Fault::invalid_argument(format!("{name} is outside bounds")));
    }
    Ok(value)
}

fn bounded_u64(name: &str, default: u64, maximum: u64) -> FaultResult<u64> {
    let value = env::var(name).ok().map_or(Ok(default), |raw| {
        raw.parse()
            .map_err(|_| Fault::invalid_argument(format!("{name} is invalid")))
    })?;
    if value == 0 || value > maximum {
        return Err(Fault::invalid_argument(format!("{name} is outside bounds")));
    }
    Ok(value)
}

fn validate_name(value: &str, label: &str) -> FaultResult<()> {
    if value.is_empty()
        || value.len() > 512
        || value.bytes().any(|byte| byte <= 0x20 || byte == 0x7f)
    {
        return Err(Fault::invalid_argument(format!("{label} is invalid")));
    }
    Ok(())
}
