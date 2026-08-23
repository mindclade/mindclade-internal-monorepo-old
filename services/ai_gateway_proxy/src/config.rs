// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Governed proxy settings, resolved through the shared configuration catalog.
//!
//! This file used to carry its own `required` / `bounded_usize` / `bounded_u64`
//! helpers and its own `Secret` newtype — the third independent copy of that
//! set in the Rust tree. All of it now comes from `mindclade_config`, which is
//! also where the `Secret` type that started here was generalized to.
//!
//! What stays here is the part that is actually about this service: the URL
//! policy (egress must be HTTPS with no embedded credentials), the endpoint
//! catalog decoded from JSON, and the cost arithmetic. Those are proxy rules,
//! not configuration plumbing.

use crate::model::{GatewayOperation, Quota, Route};
use mindclade_config::{Catalog, EnvSource, Field, Snapshot};
use mindclade_faults::{Code, Fault, FaultResult};
use reqwest::Url;
use serde::Deserialize;
use std::{
    collections::{BTreeMap, BTreeSet},
    fmt,
    net::SocketAddr,
    path::PathBuf,
    time::Duration,
};

pub use mindclade_config::Secret;

const MAX_ENDPOINTS: usize = 128;
const MAX_CA_BUNDLE_PATH_BYTES: usize = 1024;
const NAMESPACE: &str = "AI Gateway";

const DEFAULT_GOOGLE_JWKS_URL: &str = "https://www.googleapis.com/oauth2/v3/certs";
const DEFAULT_MAXIMUM_BODY_BYTES: usize = 16 * 1024 * 1024;
const DEFAULT_MAXIMUM_RESPONSE_BYTES: usize = 64 * 1024 * 1024;
const DEFAULT_MAXIMUM_CONCURRENCY: usize = 256;
const MAXIMUM_CONCURRENCY_CEILING: usize = 16_384;
const DEFAULT_PROVIDER_TIMEOUT_SECONDS: u64 = 120;
const PROVIDER_TIMEOUT_CEILING_SECONDS: u64 = 900;
const DEFAULT_CONTROL_TIMEOUT_SECONDS: u64 = 5;
const CONTROL_TIMEOUT_CEILING_SECONDS: u64 = 30;
const DEFAULT_RESERVATION_TTL_SECONDS: u32 = 300;
const RESERVATION_TTL_CEILING_SECONDS: u32 = 900;

/// Canonical configuration keys bound to their environment variable names.
///
/// This table is the migration's safety net: the variable names on the right
/// are the deployed contract, and `tests/environment_contract.rs` asserts the
/// list against a literal expectation so a rename cannot slip through as a
/// silent no-op read.
pub const BINDINGS: &[(&str, &str)] = &[
    (
        "allow.insecure.control.http",
        "MINDCLADE_AI_GATEWAY_ALLOW_INSECURE_CONTROL_HTTP",
    ),
    ("client.audience", "MINDCLADE_AI_GATEWAY_CLIENT_AUDIENCE"),
    (
        "control.timeout.seconds",
        "MINDCLADE_AI_GATEWAY_CONTROL_TIMEOUT_SECONDS",
    ),
    ("control.token", "MINDCLADE_AI_GATEWAY_CONTROL_TOKEN"),
    ("control.url", "MINDCLADE_AI_GATEWAY_CONTROL_URL"),
    (
        "egress.ca.bundle.path",
        "MINDCLADE_AI_GATEWAY_EGRESS_CA_BUNDLE_PATH",
    ),
    ("egress.proxy.url", "MINDCLADE_AI_GATEWAY_EGRESS_PROXY_URL"),
    ("endpoints", "MINDCLADE_AI_GATEWAY_ENDPOINTS"),
    ("google.jwks.url", "MINDCLADE_AI_GATEWAY_GOOGLE_JWKS_URL"),
    ("listen.address", "MINDCLADE_AI_GATEWAY_LISTEN_ADDRESS"),
    (
        "maximum.body.bytes",
        "MINDCLADE_AI_GATEWAY_MAXIMUM_BODY_BYTES",
    ),
    (
        "maximum.concurrency",
        "MINDCLADE_AI_GATEWAY_MAXIMUM_CONCURRENCY",
    ),
    (
        "maximum.response.bytes",
        "MINDCLADE_AI_GATEWAY_MAXIMUM_RESPONSE_BYTES",
    ),
    (
        "provider.timeout.seconds",
        "MINDCLADE_AI_GATEWAY_PROVIDER_TIMEOUT_SECONDS",
    ),
    (
        "reservation.ttl.seconds",
        "MINDCLADE_AI_GATEWAY_RESERVATION_TTL_SECONDS",
    ),
];

/// The complete configuration surface of the AI Gateway proxy.
///
/// Every field carries its documentation, so `catalog()?.documentation()`
/// renders the operator-facing settings reference from the same declaration the
/// process loads from.
pub fn catalog() -> FaultResult<Catalog> {
    // Required settings preserve the pre-migration policy exactly: a
    // whitespace-only value counts as missing rather than as invalid, and the
    // surrounding whitespace of a non-blank value is preserved, so a padded URL
    // still fails at the URL parser rather than earlier.
    let required =
        |key: &'static str, doc: &'static str| Field::required(key, doc).blank_is_missing();
    Catalog::new(NAMESPACE)?
        .declare(Field::defaulted(
            "allow.insecure.control.http",
            "Permit plaintext HTTP to the control plane. Enabled only by the exact \
                 value `true`; any other spelling leaves TLS required.",
            "false",
        ))?
        .declare(required(
            "listen.address",
            "Socket address the proxy listens on.",
        ))?
        .declare(required(
            "control.url",
            "Base URL of the Go control plane that owns budget policy.",
        ))?
        .declare(
            required(
                "control.token",
                "Bearer credential presented to the control plane.",
            )
            .secret(),
        )?
        .declare(required(
            "client.audience",
            "Google ID token audience accepted from callers.",
        ))?
        .declare(Field::defaulted(
            "google.jwks.url",
            "JWKS endpoint used to verify caller identity tokens.",
            DEFAULT_GOOGLE_JWKS_URL,
        ))?
        .declare(required(
            "egress.proxy.url",
            "HTTPS forward proxy every provider request egresses through.",
        ))?
        .declare(required(
            "egress.ca.bundle.path",
            "Absolute path to the CA bundle trusted for provider egress.",
        ))?
        .declare(required(
            "endpoints",
            "JSON array of governed provider endpoints, one per workspace alias.",
        ))?
        .declare(Field::defaulted(
            "maximum.body.bytes",
            "Largest request body the proxy will accept or forward.",
            DEFAULT_MAXIMUM_BODY_BYTES.to_string(),
        ))?
        .declare(Field::defaulted(
            "maximum.response.bytes",
            "Largest provider response the proxy will buffer.",
            DEFAULT_MAXIMUM_RESPONSE_BYTES.to_string(),
        ))?
        .declare(Field::defaulted(
            "maximum.concurrency",
            "Ceiling on provider requests in flight at once.",
            DEFAULT_MAXIMUM_CONCURRENCY.to_string(),
        ))?
        .declare(Field::defaulted(
            "provider.timeout.seconds",
            "Deadline for a single upstream provider request.",
            DEFAULT_PROVIDER_TIMEOUT_SECONDS.to_string(),
        ))?
        .declare(Field::defaulted(
            "control.timeout.seconds",
            "Deadline for a single control-plane reserve or reconcile call.",
            DEFAULT_CONTROL_TIMEOUT_SECONDS.to_string(),
        ))?
        .declare(Field::defaulted(
            "reservation.ttl.seconds",
            "Lifetime of a budget reservation before it must be reconciled.",
            DEFAULT_RESERVATION_TTL_SECONDS.to_string(),
        ))
}

/// An [`EnvSource`] bound to every declared key, reading the process environment.
#[must_use]
pub fn environment() -> EnvSource {
    bind(EnvSource::process())
}

fn bind(source: EnvSource) -> EnvSource {
    BINDINGS
        .iter()
        .fold(source, |bound, (key, variable)| bound.bind(*key, *variable))
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
    /// Resolves the proxy configuration from the process environment.
    pub fn from_env() -> FaultResult<Self> {
        Self::resolve(&EnvSource::process())
    }

    /// Resolves from an explicit variable table.
    ///
    /// The composition root uses [`ProxyConfig::from_env`]; this exists so the
    /// environment contract can be tested without `std::env::set_var`, which
    /// edition 2024 gates behind an audited block and which races every other
    /// test in the process.
    pub fn from_variables(variables: BTreeMap<String, String>) -> FaultResult<Self> {
        Self::resolve(&EnvSource::from_table(variables))
    }

    fn resolve(lookup: &EnvSource) -> FaultResult<Self> {
        let settings = bind(lookup.clone());
        let snapshot = catalog()?.load(&[&settings])?;

        let allow_insecure_control = snapshot.equals("allow.insecure.control.http", "true")?;
        let control_base_url = parse_base_url(
            snapshot.raw("control.url")?,
            allow_insecure_control,
            "control-plane",
        )?;
        let google_jwks_url =
            parse_base_url(snapshot.raw("google.jwks.url")?, false, "Google JWKS")?;
        let egress_proxy_url = parse_proxy_url(snapshot.raw("egress.proxy.url")?)?;
        let endpoints = endpoints_from_snapshot(lookup, &snapshot)?;

        let config = Self {
            listen_address: snapshot
                .parse::<SocketAddr>("listen.address")
                .map_err(|_| Fault::invalid_argument("AI Gateway listen address is invalid"))?,
            control_base_url,
            control_token: snapshot.secret("control.token")?,
            client_audience: snapshot.string("client.audience")?,
            google_jwks_url,
            egress_proxy_url,
            egress_ca_bundle_path: snapshot
                .resolved_absolute_path("egress.ca.bundle.path", MAX_CA_BUNDLE_PATH_BYTES)
                .map_err(|_| Fault::invalid_argument("egress CA bundle path is not allowed"))?,
            endpoints,
            maximum_body_bytes: snapshot.usize_bounded(
                "maximum.body.bytes",
                1,
                DEFAULT_MAXIMUM_BODY_BYTES,
            )?,
            maximum_response_bytes: snapshot.usize_bounded(
                "maximum.response.bytes",
                1,
                DEFAULT_MAXIMUM_RESPONSE_BYTES,
            )?,
            maximum_concurrency: snapshot.usize_bounded(
                "maximum.concurrency",
                1,
                MAXIMUM_CONCURRENCY_CEILING,
            )?,
            provider_timeout: snapshot.duration_seconds_bounded(
                "provider.timeout.seconds",
                1,
                PROVIDER_TIMEOUT_CEILING_SECONDS,
            )?,
            control_timeout: snapshot.duration_seconds_bounded(
                "control.timeout.seconds",
                1,
                CONTROL_TIMEOUT_CEILING_SECONDS,
            )?,
            reservation_ttl_seconds: snapshot.u32_bounded(
                "reservation.ttl.seconds",
                1,
                RESERVATION_TTL_CEILING_SECONDS,
            )?,
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

/// Resolves per-endpoint provider credentials in a second, derived load.
///
/// Endpoint tokens live in environment variables the operator names inside the
/// endpoint JSON, so they cannot be declared in the static catalog. They get
/// their own catalog instead of a raw `env::var`: the tokens stay `Secret`,
/// they stay bounded by [`MAX_ENDPOINTS`], and a missing one reports the same
/// fault as any other missing setting.
fn endpoints_from_snapshot(
    lookup: &EnvSource,
    snapshot: &Snapshot,
) -> FaultResult<BTreeMap<(String, String), EndpointConfig>> {
    let endpoint_inputs: Vec<EndpointInput> = serde_json::from_str(snapshot.raw("endpoints")?)
        .map_err(|_| Fault::invalid_argument("AI Gateway endpoint configuration is invalid"))?;
    if endpoint_inputs.is_empty() || endpoint_inputs.len() > MAX_ENDPOINTS {
        return Err(Fault::invalid_argument(
            "AI Gateway endpoint count is invalid",
        ));
    }
    for input in &endpoint_inputs {
        validate_endpoint_policy(input)?;
    }

    let mut token_catalog = Catalog::new("AI Gateway endpoint")?;
    let mut token_source = lookup.clone();
    let mut token_keys = Vec::with_capacity(endpoint_inputs.len());
    for (index, input) in endpoint_inputs.iter().enumerate() {
        let key = format!("provider.token.{index}");
        token_catalog = token_catalog.declare(
            Field::required(
                key.clone(),
                "Provider credential named by an endpoint's `provider_token_env`.",
            )
            .secret()
            .blank_is_missing(),
        )?;
        token_source = token_source.bind(key.clone(), input.provider_token_env.trim());
        token_keys.push(key);
    }
    let tokens = token_catalog.load(&[&token_source])?;

    let mut endpoints = BTreeMap::new();
    for (input, token_key) in endpoint_inputs.into_iter().zip(token_keys) {
        let endpoint = EndpointConfig {
            workspace_id: input.workspace_id,
            alias: input.alias,
            operations: input.operations,
            route: input.route,
            connection_ref: input.connection_ref,
            policy_epoch: input.policy_epoch,
            pricing_version: input.pricing_version,
            provider_base_url: parse_base_url(&input.provider_base_url, false, "provider")?,
            provider_token: tokens.secret(&token_key)?,
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

fn validate_endpoint_policy(input: &EndpointInput) -> FaultResult<()> {
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
        || input.maximum_body_bytes > DEFAULT_MAXIMUM_BODY_BYTES
        || input.provider_token_env.trim().is_empty()
    {
        return Err(Fault::invalid_argument(
            "AI Gateway endpoint policy is invalid",
        ));
    }
    Ok(())
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

fn validate_name(value: &str, label: &str) -> FaultResult<()> {
    if value.is_empty()
        || value.len() > 512
        || value.bytes().any(|byte| byte <= 0x20 || byte == 0x7f)
    {
        return Err(Fault::invalid_argument(format!("{label} is invalid")));
    }
    Ok(())
}
