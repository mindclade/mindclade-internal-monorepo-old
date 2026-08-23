// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! The AI Gateway environment contract, pinned against the pre-migration loader.
//!
//! `ProxyConfig::from_env` carried the third independent copy of the
//! `required` / `bounded_usize` / `bounded_u64` helper set in this tree, plus a
//! local `Secret` newtype. `EXPECTED_VARIABLES` and the bounds table below are
//! transcribed from `services/ai_gateway_proxy/src/config.rs` as it stood at
//! `origin/main`, so the migration cannot quietly stop reading a variable or
//! widen a ceiling.

use mindclade_ai_gateway_proxy::ProxyConfig;
use mindclade_ai_gateway_proxy::config;
use mindclade_faults::{Code, Fault};
use std::collections::{BTreeMap, BTreeSet};

/// Every environment variable the pre-migration loader read by a fixed name.
///
/// Per-endpoint provider credentials are excluded: their variable names come
/// from the endpoint JSON at runtime, and `endpoint_tokens_are_required_secrets`
/// covers them.
const EXPECTED_VARIABLES: &[&str] = &[
    "MINDCLADE_AI_GATEWAY_ALLOW_INSECURE_CONTROL_HTTP",
    "MINDCLADE_AI_GATEWAY_CLIENT_AUDIENCE",
    "MINDCLADE_AI_GATEWAY_CONTROL_TIMEOUT_SECONDS",
    "MINDCLADE_AI_GATEWAY_CONTROL_TOKEN",
    "MINDCLADE_AI_GATEWAY_CONTROL_URL",
    "MINDCLADE_AI_GATEWAY_EGRESS_CA_BUNDLE_PATH",
    "MINDCLADE_AI_GATEWAY_EGRESS_PROXY_URL",
    "MINDCLADE_AI_GATEWAY_ENDPOINTS",
    "MINDCLADE_AI_GATEWAY_GOOGLE_JWKS_URL",
    "MINDCLADE_AI_GATEWAY_LISTEN_ADDRESS",
    "MINDCLADE_AI_GATEWAY_MAXIMUM_BODY_BYTES",
    "MINDCLADE_AI_GATEWAY_MAXIMUM_CONCURRENCY",
    "MINDCLADE_AI_GATEWAY_MAXIMUM_RESPONSE_BYTES",
    "MINDCLADE_AI_GATEWAY_PROVIDER_TIMEOUT_SECONDS",
    "MINDCLADE_AI_GATEWAY_RESERVATION_TTL_SECONDS",
];

/// Variables the old `required()` refused to start without.
const REQUIRED_VARIABLES: &[&str] = &[
    "MINDCLADE_AI_GATEWAY_CLIENT_AUDIENCE",
    "MINDCLADE_AI_GATEWAY_CONTROL_TOKEN",
    "MINDCLADE_AI_GATEWAY_CONTROL_URL",
    "MINDCLADE_AI_GATEWAY_EGRESS_CA_BUNDLE_PATH",
    "MINDCLADE_AI_GATEWAY_EGRESS_PROXY_URL",
    "MINDCLADE_AI_GATEWAY_ENDPOINTS",
    "MINDCLADE_AI_GATEWAY_LISTEN_ADDRESS",
];

const PROVIDER_TOKEN_VARIABLE: &str = "MINDCLADE_TEST_PROVIDER_TOKEN";

fn endpoints_json() -> String {
    format!(
        r#"[{{
            "workspace_id": "workspace-1",
            "alias": "chat",
            "operations": ["chat_completions"],
            "route": {{"endpoint": "chat", "provider": "openai", "model": "gpt-4o"}},
            "connection_ref": "connection-1",
            "policy_epoch": 1,
            "pricing_version": 1,
            "provider_base_url": "https://api.example.com",
            "provider_token_env": "{PROVIDER_TOKEN_VARIABLE}",
            "reservation": {{
                "requests": 1,
                "input_tokens": 1000,
                "output_tokens": 1000,
                "cost_micros": 100000
            }},
            "request_micros": 100,
            "input_micros_per_million": 1000,
            "output_micros_per_million": 2000,
            "maximum_body_bytes": 1048576
        }}]"#
    )
}

fn valid() -> BTreeMap<String, String> {
    [
        ("MINDCLADE_AI_GATEWAY_LISTEN_ADDRESS", "127.0.0.1:8443"),
        (
            "MINDCLADE_AI_GATEWAY_CONTROL_URL",
            "https://control.example.com",
        ),
        ("MINDCLADE_AI_GATEWAY_CONTROL_TOKEN", "control-plane-secret"),
        (
            "MINDCLADE_AI_GATEWAY_CLIENT_AUDIENCE",
            "mindclade-ai-gateway",
        ),
        (
            "MINDCLADE_AI_GATEWAY_EGRESS_PROXY_URL",
            "https://egress.example.com:3128",
        ),
        (
            "MINDCLADE_AI_GATEWAY_EGRESS_CA_BUNDLE_PATH",
            "/etc/mindclade/egress-ca.pem",
        ),
        (PROVIDER_TOKEN_VARIABLE, "provider-secret"),
    ]
    .into_iter()
    .map(|(key, value)| (key.to_owned(), value.to_owned()))
    .chain(std::iter::once((
        "MINDCLADE_AI_GATEWAY_ENDPOINTS".to_owned(),
        endpoints_json(),
    )))
    .collect()
}

fn with(key: &str, value: &str) -> BTreeMap<String, String> {
    let mut variables = valid();
    variables.insert(key.to_owned(), value.to_owned());
    variables
}

fn without(key: &str) -> BTreeMap<String, String> {
    let mut variables = valid();
    variables.remove(key);
    variables
}

fn failure(variables: BTreeMap<String, String>) -> Fault {
    ProxyConfig::from_variables(variables).expect_err("expected a configuration failure")
}

#[test]
fn every_pre_migration_variable_is_still_read() {
    let bound: BTreeSet<&str> = config::BINDINGS
        .iter()
        .map(|(_, variable)| *variable)
        .collect();
    let expected: BTreeSet<&str> = EXPECTED_VARIABLES.iter().copied().collect();

    let dropped: Vec<&&str> = expected.difference(&bound).collect();
    assert!(
        dropped.is_empty(),
        "variables the process no longer reads: {dropped:?}"
    );
    let added: Vec<&&str> = bound.difference(&expected).collect();
    assert!(
        added.is_empty(),
        "variables added without updating this contract: {added:?}"
    );

    let catalog = config::catalog().expect("catalog");
    assert_eq!(catalog.fields().count(), config::BINDINGS.len());
    for (key, _) in config::BINDINGS {
        assert!(catalog.field(key).is_some(), "unbound catalog key {key}");
    }
}

#[test]
fn a_fully_specified_environment_resolves_with_the_documented_defaults() {
    let config = ProxyConfig::from_variables(valid()).expect("valid environment");
    assert_eq!(config.client_audience, "mindclade-ai-gateway");
    assert_eq!(config.maximum_body_bytes, 16 * 1024 * 1024);
    assert_eq!(config.maximum_response_bytes, 64 * 1024 * 1024);
    assert_eq!(config.maximum_concurrency, 256);
    assert_eq!(config.provider_timeout.as_secs(), 120);
    assert_eq!(config.control_timeout.as_secs(), 5);
    assert_eq!(config.reservation_ttl_seconds, 300);
    assert_eq!(
        config.google_jwks_url.as_str(),
        "https://www.googleapis.com/oauth2/v3/certs"
    );
    assert_eq!(config.endpoints.len(), 1);
}

#[test]
fn every_required_variable_is_fatal_when_absent_or_blank() {
    for variable in REQUIRED_VARIABLES {
        let absent = failure(without(variable));
        assert_eq!(
            absent.code(),
            Code::FailedPrecondition,
            "{variable}: absent must be FailedPrecondition, got {absent}"
        );
        // The old `required()` filtered on `!value.trim().is_empty()`, so a
        // whitespace-only value read as *missing* rather than as invalid. That
        // is the opposite of runtime-host's rule, and both are preserved.
        for value in ["", "   "] {
            let blank = failure(with(variable, value));
            assert_eq!(
                blank.code(),
                Code::FailedPrecondition,
                "{variable}={value:?} must be FailedPrecondition, got {blank}"
            );
        }
    }
}

#[test]
fn numeric_ceilings_are_preserved() {
    // (variable, default, ceiling) exactly as the old bounded_* calls declared.
    let cases: &[(&str, u64, u64)] = &[
        (
            "MINDCLADE_AI_GATEWAY_MAXIMUM_BODY_BYTES",
            16 * 1024 * 1024,
            16 * 1024 * 1024,
        ),
        (
            "MINDCLADE_AI_GATEWAY_MAXIMUM_RESPONSE_BYTES",
            64 * 1024 * 1024,
            64 * 1024 * 1024,
        ),
        ("MINDCLADE_AI_GATEWAY_MAXIMUM_CONCURRENCY", 256, 16_384),
        ("MINDCLADE_AI_GATEWAY_PROVIDER_TIMEOUT_SECONDS", 120, 900),
        ("MINDCLADE_AI_GATEWAY_CONTROL_TIMEOUT_SECONDS", 5, 30),
        ("MINDCLADE_AI_GATEWAY_RESERVATION_TTL_SECONDS", 300, 900),
    ];
    for (variable, default, ceiling) in cases {
        // Absent resolves to the declared default.
        ProxyConfig::from_variables(without(variable))
            .unwrap_or_else(|error| panic!("{variable} absent must resolve: {error}"));
        assert!(default <= ceiling, "{variable}: default above its ceiling");

        // The ceiling itself is accepted; one past it is not.
        ProxyConfig::from_variables(with(variable, &ceiling.to_string()))
            .unwrap_or_else(|error| panic!("{variable}={ceiling} must resolve: {error}"));
        for value in ["0", &(ceiling + 1).to_string(), "", "not-a-number", "-1"] {
            let fault = failure(with(variable, value));
            assert_eq!(
                fault.code(),
                Code::InvalidArgument,
                "{variable}={value:?} must be InvalidArgument, got {fault}"
            );
        }
    }
}

#[test]
fn insecure_control_transport_needs_the_exact_literal_true() {
    let insecure = |flag: Option<&str>| -> BTreeMap<String, String> {
        let mut variables = valid();
        variables.insert(
            "MINDCLADE_AI_GATEWAY_CONTROL_URL".to_owned(),
            "http://control.internal".to_owned(),
        );
        match flag {
            Some(value) => {
                variables.insert(
                    "MINDCLADE_AI_GATEWAY_ALLOW_INSECURE_CONTROL_HTTP".to_owned(),
                    value.to_owned(),
                );
            }
            None => {
                variables.remove("MINDCLADE_AI_GATEWAY_ALLOW_INSECURE_CONTROL_HTTP");
            }
        }
        variables
    };

    assert!(ProxyConfig::from_variables(insecure(Some("true"))).is_ok());
    // Anything else leaves TLS required. A permissive boolean parser here would
    // turn a typo in a deployment manifest into a silent transport downgrade.
    for value in ["TRUE", "True", "1", "yes", "", " true "] {
        assert!(
            ProxyConfig::from_variables(insecure(Some(value))).is_err(),
            "{value:?} must not enable plaintext control transport"
        );
    }
    assert!(ProxyConfig::from_variables(insecure(None)).is_err());
    // ...and the flag never permits plaintext anywhere else.
    assert!(
        ProxyConfig::from_variables(with(
            "MINDCLADE_AI_GATEWAY_GOOGLE_JWKS_URL",
            "http://jwks.internal"
        ))
        .is_err()
    );
}

#[test]
fn url_and_path_policy_is_preserved() {
    let cases: &[(&str, &str)] = &[
        (
            "MINDCLADE_AI_GATEWAY_CONTROL_URL",
            "http://control.internal",
        ),
        ("MINDCLADE_AI_GATEWAY_CONTROL_URL", "not-a-url"),
        (
            "MINDCLADE_AI_GATEWAY_CONTROL_URL",
            "https://user:pass@control.example.com",
        ),
        (
            "MINDCLADE_AI_GATEWAY_CONTROL_URL",
            "https://control.example.com?query=1",
        ),
        (
            "MINDCLADE_AI_GATEWAY_EGRESS_PROXY_URL",
            "http://egress.example.com:3128",
        ),
        (
            "MINDCLADE_AI_GATEWAY_EGRESS_PROXY_URL",
            "https://egress.example.com:3128#fragment",
        ),
        ("MINDCLADE_AI_GATEWAY_EGRESS_CA_BUNDLE_PATH", "relative.pem"),
        (
            "MINDCLADE_AI_GATEWAY_EGRESS_CA_BUNDLE_PATH",
            "/etc/mindclade/../../secrets/ca.pem",
        ),
        ("MINDCLADE_AI_GATEWAY_LISTEN_ADDRESS", "127.0.0.1"),
        ("MINDCLADE_AI_GATEWAY_LISTEN_ADDRESS", " 127.0.0.1:8443 "),
    ];
    for (variable, value) in cases {
        let fault = failure(with(variable, value));
        assert_eq!(
            fault.code(),
            Code::InvalidArgument,
            "{variable}={value:?} must be InvalidArgument, got {fault}"
        );
    }

    // A bare `.` component is NOT rejected, and was not rejected before either:
    // `std::path::Components` normalizes it away, so the traversal guard never
    // sees one. Pinned rather than fixed — it cannot escape a directory the way
    // `..` can, and quietly tightening it here would be an unreviewed change to
    // what a deployed CA-bundle path may look like.
    assert!(
        ProxyConfig::from_variables(with(
            "MINDCLADE_AI_GATEWAY_EGRESS_CA_BUNDLE_PATH",
            "/etc/./mindclade/ca.pem"
        ))
        .is_ok()
    );

    // A CA bundle path over the 1 KiB ceiling the old parse_secret_path enforced.
    let long = format!("/etc/{}", "a".repeat(1030));
    assert_eq!(
        failure(with("MINDCLADE_AI_GATEWAY_EGRESS_CA_BUNDLE_PATH", &long)).code(),
        Code::InvalidArgument
    );
}

#[test]
fn endpoint_tokens_are_required_secrets_named_by_the_endpoint_json() {
    let config = ProxyConfig::from_variables(valid()).expect("valid environment");
    let endpoint = config
        .endpoints
        .values()
        .next()
        .expect("one configured endpoint");
    assert_eq!(endpoint.provider_token.expose(), "provider-secret");

    // The variable the JSON names is genuinely required.
    let fault = failure(without(PROVIDER_TOKEN_VARIABLE));
    assert_eq!(fault.code(), Code::FailedPrecondition);
    assert_eq!(
        failure(with(PROVIDER_TOKEN_VARIABLE, "   ")).code(),
        Code::FailedPrecondition
    );
}

#[test]
fn endpoint_policy_bounds_are_preserved() {
    let cases: &[(&str, &str)] = &[
        ("\"policy_epoch\": 1", "\"policy_epoch\": 0"),
        ("\"pricing_version\": 1", "\"pricing_version\": 0"),
        (
            "\"maximum_body_bytes\": 1048576",
            "\"maximum_body_bytes\": 0",
        ),
        (
            "\"maximum_body_bytes\": 1048576",
            "\"maximum_body_bytes\": 16777217",
        ),
        ("\"requests\": 1", "\"requests\": 2"),
        ("\"alias\": \"chat\"", "\"alias\": \"other\""),
        (
            "\"provider_token_env\": \"MINDCLADE_TEST_PROVIDER_TOKEN\"",
            "\"provider_token_env\": \"  \"",
        ),
        (
            "\"provider_base_url\": \"https://api.example.com\"",
            "\"provider_base_url\": \"http://api.example.com\"",
        ),
    ];
    for (from, to) in cases {
        let json = endpoints_json().replace(from, to);
        assert_ne!(json, endpoints_json(), "case {to} did not apply");
        let fault = failure(with("MINDCLADE_AI_GATEWAY_ENDPOINTS", &json));
        assert_eq!(
            fault.code(),
            Code::InvalidArgument,
            "{to} must be InvalidArgument, got {fault}"
        );
    }

    assert_eq!(
        failure(with("MINDCLADE_AI_GATEWAY_ENDPOINTS", "[]")).code(),
        Code::InvalidArgument
    );
    assert_eq!(
        failure(with("MINDCLADE_AI_GATEWAY_ENDPOINTS", "not json")).code(),
        Code::InvalidArgument
    );
}

#[test]
fn no_secret_reaches_debug_output() {
    let config = ProxyConfig::from_variables(valid()).expect("valid environment");
    let rendered = format!("{config:?}");
    assert!(
        !rendered.contains("control-plane-secret"),
        "ProxyConfig Debug leaked the control token: {rendered}"
    );
    assert!(rendered.contains("[REDACTED]"));
    assert_eq!(format!("{:?}", config.control_token), "[REDACTED]");
    assert_eq!(format!("{}", config.control_token), "[REDACTED]");

    let endpoint = config
        .endpoints
        .values()
        .next()
        .expect("one configured endpoint");
    let rendered = format!("{endpoint:?}");
    assert!(
        !rendered.contains("provider-secret"),
        "EndpointConfig Debug leaked the provider token: {rendered}"
    );
}

#[test]
fn an_undeclared_key_is_rejected_rather_than_ignored() {
    use mindclade_config::MapSource;

    let source = MapSource::new("file").with("controll.url", "https://control.example.com");
    let fault = config::catalog()
        .expect("catalog")
        .load(&[&source])
        .expect_err("undeclared key");
    assert_eq!(fault.code(), Code::InvalidArgument);
    assert_eq!(
        fault.context().get("reason").map(ToString::to_string),
        Some(mindclade_config::reason::KEY_UNKNOWN.to_owned())
    );
}

#[test]
fn the_settings_surface_is_documented() {
    let catalog = config::catalog().expect("catalog");
    let rendered = catalog.documentation();
    for (key, _) in config::BINDINGS {
        assert!(rendered.contains(key), "undocumented setting {key}");
    }
    for field in catalog.fields() {
        assert!(
            field.doc().len() > 20,
            "{}: documentation is too thin to be useful",
            field.key()
        );
    }
    // The control token is declared secret, so the rendered surface names it
    // without ever being able to carry its value.
    assert!(
        catalog
            .field("control.token")
            .expect("control.token")
            .is_secret()
    );
}
