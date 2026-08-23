// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Fault construction for configuration failures.
//!
//! Every fault carries a machine-readable `reason`, mirroring the
//! `faults.WithReason` convention in `libs/go/config`, so a caller can classify
//! a failure without matching on message text. The failing key and, where one
//! exists, the environment variable name are attached as context. **Values are
//! never attached** — a configuration value may be a credential, and a fault is
//! the single most likely thing in a startup path to reach a log.

use mindclade_faults::{Code, Fault};

/// Stable `reason` values attached to configuration faults.
pub mod reason {
    /// A field definition itself is malformed (key, documentation, or bound).
    pub const FIELD_INVALID: &str = "config_field_invalid";
    /// A source produced a key the catalog does not declare.
    pub const KEY_UNKNOWN: &str = "config_key_unknown";
    /// A required value was absent from every source.
    pub const VALUE_MISSING: &str = "config_value_missing";
    /// A value was present but violates its declared shape or bound.
    pub const VALUE_INVALID: &str = "config_value_invalid";
    /// A value parsed but does not fit the target width or range.
    pub const VALUE_OUT_OF_RANGE: &str = "config_value_out_of_range";
    /// A source failed to produce its values.
    pub const SOURCE_FAILED: &str = "config_source_failed";
    /// A reload changed a field that is not marked reloadable.
    pub const RESTART_REQUIRED: &str = "config_restart_required";
}

/// Context key naming the configuration key that failed.
pub(crate) const CONTEXT_KEY: &str = "key";
/// Context key naming the environment variable a key is bound to.
pub(crate) const CONTEXT_VARIABLE: &str = "variable";
/// Context key naming the source a value came from.
pub(crate) const CONTEXT_SOURCE: &str = "source";
/// Context key naming the owning configuration namespace.
pub(crate) const CONTEXT_NAMESPACE: &str = "namespace";

fn base(code: Code, namespace: &str, reason: &'static str, message: String) -> Fault {
    Fault::new(code, message)
        .with_context("reason", reason)
        .with_context(CONTEXT_NAMESPACE, namespace)
}

pub(crate) fn field_invalid(namespace: &str, key: &str, detail: &str) -> Fault {
    base(
        Code::InvalidArgument,
        namespace,
        reason::FIELD_INVALID,
        format!("{namespace} configuration field is invalid: {detail}"),
    )
    .with_context(CONTEXT_KEY, key)
}

pub(crate) fn unknown_key(namespace: &str, key: &str, source: &str) -> Fault {
    base(
        Code::InvalidArgument,
        namespace,
        reason::KEY_UNKNOWN,
        format!("{namespace} configuration source supplied an undeclared key"),
    )
    .with_context(CONTEXT_KEY, key)
    .with_context(CONTEXT_SOURCE, source)
}

pub(crate) fn missing(namespace: &str, key: &str, variable: Option<&str>) -> Fault {
    let fault = base(
        Code::FailedPrecondition,
        namespace,
        reason::VALUE_MISSING,
        format!("required {namespace} setting is missing"),
    )
    .with_context(CONTEXT_KEY, key);
    match variable {
        Some(name) => fault.with_context(CONTEXT_VARIABLE, name),
        None => fault,
    }
}

pub(crate) fn invalid(namespace: &str, key: &str, detail: &str) -> Fault {
    base(
        Code::InvalidArgument,
        namespace,
        reason::VALUE_INVALID,
        format!("{namespace} setting is invalid: {detail}"),
    )
    .with_context(CONTEXT_KEY, key)
}

pub(crate) fn out_of_range(namespace: &str, key: &str, detail: &str) -> Fault {
    base(
        Code::OutOfRange,
        namespace,
        reason::VALUE_OUT_OF_RANGE,
        format!("{namespace} setting is out of range: {detail}"),
    )
    .with_context(CONTEXT_KEY, key)
}

pub(crate) fn source_failed(namespace: &str, source: &str, detail: &str) -> Fault {
    base(
        Code::Unavailable,
        namespace,
        reason::SOURCE_FAILED,
        format!("{namespace} configuration source failed: {detail}"),
    )
    .with_context(CONTEXT_SOURCE, source)
}

pub(crate) fn restart_required(namespace: &str, key: &str) -> Fault {
    base(
        Code::FailedPrecondition,
        namespace,
        reason::RESTART_REQUIRED,
        format!("{namespace} configuration change requires a restart"),
    )
    .with_context(CONTEXT_KEY, key)
}

pub(crate) fn internal(namespace: &str, detail: &str) -> Fault {
    base(
        Code::Internal,
        namespace,
        reason::SOURCE_FAILED,
        format!("{namespace} configuration state is unusable: {detail}"),
    )
}
