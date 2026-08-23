// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! The runtime-host environment contract, pinned against the pre-migration loader.
//!
//! `BootstrapConfig::from_env` used to be 70 lines of private `required` /
//! `parse_u64` / `parse_positive_u32` / `absolute_path` helpers. Moving it onto
//! `mindclade_config` is only safe if every variable it read is still read,
//! under the same name, with the same bounds and the same failure code — a
//! dropped variable is a silent production outage that no compiler and no
//! architecture gate can see.
//!
//! So this suite is deliberately literal. `EXPECTED_VARIABLES` is transcribed
//! from `services/runtime_host/src/bootstrap.rs` as it stood at
//! `origin/main` before the migration, and the bounds table below restates each
//! rejection the old helpers performed. Deriving either from the new code would
//! make the test agree with whatever the code does, which is the one thing it
//! must not do.

use mindclade_faults::{Code, Fault};
use mindclade_runtime_host::bootstrap::BootstrapConfig;
use mindclade_runtime_host::config;
use std::collections::{BTreeMap, BTreeSet};

/// Every environment variable the pre-migration bootstrap read.
///
/// The two `MINDCLADE_MODEL_WORKER_*` names that also appear in the old file are
/// absent on purpose: those are *written* into the model worker's environment by
/// `build_model_spec`, never read from this process's own.
const EXPECTED_VARIABLES: &[&str] = &[
    "MINDCLADE_RUNTIME_GPU_ARCH",
    "MINDCLADE_RUNTIME_GPU_MEMORY_BYTES",
    "MINDCLADE_RUNTIME_GPU_VENDOR",
    "MINDCLADE_RUNTIME_HOST_GRPC_SOCKET",
    "MINDCLADE_RUNTIME_HOST_SOCKET",
    "MINDCLADE_RUNTIME_KEY_ID",
    "MINDCLADE_RUNTIME_KEY_NOT_AFTER_MS",
    "MINDCLADE_RUNTIME_KEY_NOT_BEFORE_MS",
    "MINDCLADE_RUNTIME_MAX_CONTROL_BYTES",
    "MINDCLADE_RUNTIME_MAX_INPUT_BUFFERS",
    "MINDCLADE_RUNTIME_MAX_MODEL_SLOTS",
    "MINDCLADE_RUNTIME_MAX_PROCESSES",
    "MINDCLADE_RUNTIME_MIN_POLICY_EPOCH",
    "MINDCLADE_RUNTIME_MIN_REVOCATION_EPOCH",
    "MINDCLADE_RUNTIME_MIN_ROUTE_VERSION",
    "MINDCLADE_RUNTIME_MODEL_BUNDLE_DIGEST",
    "MINDCLADE_RUNTIME_MODEL_GPU_MEMORY_BYTES",
    "MINDCLADE_RUNTIME_MODEL_PINNED_MEMORY_BYTES",
    "MINDCLADE_RUNTIME_MODEL_WORKER_CONFIG",
    "MINDCLADE_RUNTIME_MODEL_WORKER_EXECUTABLE",
    "MINDCLADE_RUNTIME_MODEL_WORKER_SOCKET",
    "MINDCLADE_RUNTIME_NODE_CHECKPOINT_STAGING_BYTES",
    "MINDCLADE_RUNTIME_NODE_CPU_MILLIS",
    "MINDCLADE_RUNTIME_NODE_CPU_THREADS",
    "MINDCLADE_RUNTIME_NODE_DISK_BYTES",
    "MINDCLADE_RUNTIME_NODE_GPU_MEMORY_BYTES",
    "MINDCLADE_RUNTIME_NODE_MEMORY_BYTES",
    "MINDCLADE_RUNTIME_NODE_OBJECT_REQUESTS",
    "MINDCLADE_RUNTIME_NODE_OPEN_FDS",
    "MINDCLADE_RUNTIME_NODE_PINNED_MEMORY_BYTES",
    "MINDCLADE_RUNTIME_NODE_PROCESSES",
    "MINDCLADE_RUNTIME_NODE_QUEUED_REQUESTS",
    "MINDCLADE_RUNTIME_NODE_SHARED_MEMORY_BYTES",
    "MINDCLADE_RUNTIME_NODE_TELEMETRY_SPOOL_BYTES",
    "MINDCLADE_RUNTIME_PUBLIC_KEY_HEX",
    "MINDCLADE_RUNTIME_REVOCATION_SNAPSHOT_FILE",
];

/// Variables the old loader read through `required()`: absent means the process
/// refuses to start, with `FailedPrecondition`.
const REQUIRED_VARIABLES: &[&str] = &[
    "MINDCLADE_RUNTIME_GPU_ARCH",
    "MINDCLADE_RUNTIME_GPU_MEMORY_BYTES",
    "MINDCLADE_RUNTIME_GPU_VENDOR",
    "MINDCLADE_RUNTIME_HOST_GRPC_SOCKET",
    "MINDCLADE_RUNTIME_HOST_SOCKET",
    "MINDCLADE_RUNTIME_KEY_ID",
    "MINDCLADE_RUNTIME_KEY_NOT_AFTER_MS",
    "MINDCLADE_RUNTIME_KEY_NOT_BEFORE_MS",
    "MINDCLADE_RUNTIME_MAX_CONTROL_BYTES",
    "MINDCLADE_RUNTIME_MAX_INPUT_BUFFERS",
    "MINDCLADE_RUNTIME_MAX_MODEL_SLOTS",
    "MINDCLADE_RUNTIME_MAX_PROCESSES",
    "MINDCLADE_RUNTIME_MIN_POLICY_EPOCH",
    "MINDCLADE_RUNTIME_MIN_REVOCATION_EPOCH",
    "MINDCLADE_RUNTIME_MIN_ROUTE_VERSION",
    "MINDCLADE_RUNTIME_NODE_CPU_MILLIS",
    "MINDCLADE_RUNTIME_NODE_CPU_THREADS",
    "MINDCLADE_RUNTIME_NODE_GPU_MEMORY_BYTES",
    "MINDCLADE_RUNTIME_NODE_MEMORY_BYTES",
    "MINDCLADE_RUNTIME_NODE_OPEN_FDS",
    "MINDCLADE_RUNTIME_NODE_PROCESSES",
    "MINDCLADE_RUNTIME_PUBLIC_KEY_HEX",
    "MINDCLADE_RUNTIME_REVOCATION_SNAPSHOT_FILE",
];

/// Variables the old loader read through `parse_optional_u64()`: absent or empty
/// means zero, and zero means "unconstrained".
const OPTIONAL_AMOUNTS: &[&str] = &[
    "MINDCLADE_RUNTIME_NODE_CHECKPOINT_STAGING_BYTES",
    "MINDCLADE_RUNTIME_NODE_DISK_BYTES",
    "MINDCLADE_RUNTIME_NODE_OBJECT_REQUESTS",
    "MINDCLADE_RUNTIME_NODE_PINNED_MEMORY_BYTES",
    "MINDCLADE_RUNTIME_NODE_QUEUED_REQUESTS",
    "MINDCLADE_RUNTIME_NODE_SHARED_MEMORY_BYTES",
    "MINDCLADE_RUNTIME_NODE_TELEMETRY_SPOOL_BYTES",
];

fn valid() -> BTreeMap<String, String> {
    [
        ("MINDCLADE_RUNTIME_HOST_SOCKET", "/run/mindclade/host.sock"),
        (
            "MINDCLADE_RUNTIME_HOST_GRPC_SOCKET",
            "/run/mindclade/host-grpc.sock",
        ),
        ("MINDCLADE_RUNTIME_KEY_ID", "runtime-key-1"),
        (
            "MINDCLADE_RUNTIME_PUBLIC_KEY_HEX",
            "abababababababababababababababababababababababababababababababab",
        ),
        ("MINDCLADE_RUNTIME_KEY_NOT_BEFORE_MS", "1000"),
        ("MINDCLADE_RUNTIME_KEY_NOT_AFTER_MS", "2000"),
        (
            "MINDCLADE_RUNTIME_REVOCATION_SNAPSHOT_FILE",
            "/etc/mindclade/revocations.pb",
        ),
        ("MINDCLADE_RUNTIME_MIN_POLICY_EPOCH", "1"),
        ("MINDCLADE_RUNTIME_MIN_ROUTE_VERSION", "1"),
        ("MINDCLADE_RUNTIME_MIN_REVOCATION_EPOCH", "1"),
        ("MINDCLADE_RUNTIME_MAX_PROCESSES", "8"),
        ("MINDCLADE_RUNTIME_MAX_MODEL_SLOTS", "4"),
        ("MINDCLADE_RUNTIME_MAX_INPUT_BUFFERS", "64"),
        ("MINDCLADE_RUNTIME_MAX_CONTROL_BYTES", "1048576"),
        ("MINDCLADE_RUNTIME_NODE_CPU_MILLIS", "64000"),
        ("MINDCLADE_RUNTIME_NODE_MEMORY_BYTES", "137438953472"),
        ("MINDCLADE_RUNTIME_NODE_OPEN_FDS", "65536"),
        ("MINDCLADE_RUNTIME_NODE_PROCESSES", "256"),
        ("MINDCLADE_RUNTIME_NODE_CPU_THREADS", "64"),
        ("MINDCLADE_RUNTIME_NODE_GPU_MEMORY_BYTES", "85899345920"),
        ("MINDCLADE_RUNTIME_GPU_VENDOR", "nvidia"),
        ("MINDCLADE_RUNTIME_GPU_ARCH", "hopper"),
        ("MINDCLADE_RUNTIME_GPU_MEMORY_BYTES", "85899345920"),
    ]
    .into_iter()
    .map(|(key, value)| (key.to_owned(), value.to_owned()))
    .collect()
}

fn with_model(mut variables: BTreeMap<String, String>) -> BTreeMap<String, String> {
    for (key, value) in [
        (
            "MINDCLADE_RUNTIME_MODEL_BUNDLE_DIGEST",
            "sha256:abababababababababababababababababababababababababababababababab",
        ),
        (
            "MINDCLADE_RUNTIME_MODEL_WORKER_EXECUTABLE",
            "/opt/mindclade/model-worker",
        ),
        (
            "MINDCLADE_RUNTIME_MODEL_WORKER_SOCKET",
            "/run/mindclade/model-worker.sock",
        ),
        (
            "MINDCLADE_RUNTIME_MODEL_WORKER_CONFIG",
            "/etc/mindclade/model-worker.json",
        ),
        ("MINDCLADE_RUNTIME_MODEL_GPU_MEMORY_BYTES", "8589934592"),
    ] {
        variables.insert(key.to_owned(), value.to_owned());
    }
    variables
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
    BootstrapConfig::from_variables(variables).expect_err("expected a configuration failure")
}

#[test]
fn every_pre_migration_variable_is_still_read() {
    let bound: BTreeSet<&str> = config::BINDINGS
        .iter()
        .chain(config::MODEL_BINDINGS)
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
}

#[test]
fn every_key_is_bound_exactly_once_and_declared() {
    let catalog = config::catalog().expect("catalog");
    let model = config::model_catalog().expect("model catalog");
    for (key, _) in config::BINDINGS {
        assert!(catalog.field(key).is_some(), "unbound catalog key {key}");
    }
    for (key, _) in config::MODEL_BINDINGS {
        assert!(model.field(key).is_some(), "unbound model key {key}");
    }
    assert_eq!(catalog.fields().count(), config::BINDINGS.len());
    assert_eq!(model.fields().count(), config::MODEL_BINDINGS.len());

    let variables: BTreeSet<&str> = config::BINDINGS
        .iter()
        .chain(config::MODEL_BINDINGS)
        .map(|(_, variable)| *variable)
        .collect();
    assert_eq!(
        variables.len(),
        config::BINDINGS.len() + config::MODEL_BINDINGS.len(),
        "two keys share one environment variable"
    );
}

#[test]
fn a_fully_specified_environment_resolves() {
    let config = BootstrapConfig::from_variables(valid()).expect("valid environment");
    assert_eq!(config.key_id, "runtime-key-1");
    assert_eq!(config.public_key, [0xab; 32]);
    assert_eq!(config.host.maximum_processes, 8);
    assert_eq!(config.host.maximum_model_slots, 4);
    assert_eq!(config.host.maximum_input_buffers, 64);
    assert_eq!(config.host.maximum_control_payload_bytes, 1_048_576);
    assert_eq!(config.gpu.vendor, "nvidia");
    assert_eq!(config.gpu.total_memory_bytes, 85_899_345_920);
    assert!(config.preloaded_model.is_none());
}

#[test]
fn every_required_variable_is_fatal_when_absent() {
    for variable in REQUIRED_VARIABLES {
        let fault = failure(without(variable));
        assert_eq!(
            fault.code(),
            Code::FailedPrecondition,
            "{variable}: absent required variable must be FailedPrecondition, got {fault}"
        );
    }
}

#[test]
fn every_required_variable_rejects_an_empty_or_padded_value() {
    for variable in REQUIRED_VARIABLES {
        // The old `required()` accepted the variable's presence and then
        // rejected the value: empty and whitespace-padded are *invalid*, not
        // *missing*, and the distinction is what an operator sees first.
        for value in ["", "   ", " padded "] {
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
fn optional_amounts_default_to_zero_when_absent_or_empty() {
    for variable in OPTIONAL_AMOUNTS {
        BootstrapConfig::from_variables(without(variable))
            .unwrap_or_else(|error| panic!("{variable} absent must resolve: {error}"));
        BootstrapConfig::from_variables(with(variable, ""))
            .unwrap_or_else(|error| panic!("{variable} empty must resolve: {error}"));
        let fault = failure(with(variable, "not-a-number"));
        assert_eq!(
            fault.code(),
            Code::InvalidArgument,
            "{variable}: a non-numeric value must be InvalidArgument"
        );
    }
}

#[test]
fn positive_integer_bounds_are_preserved() {
    // Zero was rejected by `parse_positive_u64`/`_u32`/`_usize`.
    for variable in [
        "MINDCLADE_RUNTIME_MIN_POLICY_EPOCH",
        "MINDCLADE_RUNTIME_MIN_ROUTE_VERSION",
        "MINDCLADE_RUNTIME_MIN_REVOCATION_EPOCH",
        "MINDCLADE_RUNTIME_MAX_PROCESSES",
        "MINDCLADE_RUNTIME_MAX_MODEL_SLOTS",
        "MINDCLADE_RUNTIME_MAX_INPUT_BUFFERS",
        "MINDCLADE_RUNTIME_MAX_CONTROL_BYTES",
        "MINDCLADE_RUNTIME_NODE_CPU_MILLIS",
        "MINDCLADE_RUNTIME_NODE_MEMORY_BYTES",
        "MINDCLADE_RUNTIME_NODE_OPEN_FDS",
        "MINDCLADE_RUNTIME_NODE_PROCESSES",
        "MINDCLADE_RUNTIME_NODE_CPU_THREADS",
        "MINDCLADE_RUNTIME_NODE_GPU_MEMORY_BYTES",
        "MINDCLADE_RUNTIME_GPU_MEMORY_BYTES",
    ] {
        let fault = failure(with(variable, "0"));
        assert_eq!(
            fault.code(),
            Code::InvalidArgument,
            "{variable}=0 must be InvalidArgument, got {fault}"
        );
    }

    // `parse_positive_u32` reported a too-wide value as OutOfRange, not
    // InvalidArgument. The distinction survives: the value parsed, it just does
    // not fit the field.
    for variable in [
        "MINDCLADE_RUNTIME_MAX_PROCESSES",
        "MINDCLADE_RUNTIME_MAX_MODEL_SLOTS",
    ] {
        let fault = failure(with(variable, "5000000000"));
        assert_eq!(
            fault.code(),
            Code::OutOfRange,
            "{variable}: a value wider than u32 must be OutOfRange, got {fault}"
        );
    }
}

#[test]
fn structural_invariants_are_preserved() {
    // Each of these was an explicit rejection in the old `from_env`.
    let cases: &[(&str, &str)] = &[
        ("MINDCLADE_RUNTIME_HOST_SOCKET", "relative/host.sock"),
        ("MINDCLADE_RUNTIME_REVOCATION_SNAPSHOT_FILE", "relative.pb"),
        ("MINDCLADE_RUNTIME_PUBLIC_KEY_HEX", "abcd"),
        ("MINDCLADE_RUNTIME_PUBLIC_KEY_HEX", &"zz".repeat(32)),
        ("MINDCLADE_RUNTIME_KEY_ID", &"k".repeat(257)),
        (
            "MINDCLADE_RUNTIME_HOST_SOCKET",
            &format!("/{}", "s".repeat(120)),
        ),
        // The control payload ceiling is the IPC maximum, enforced by
        // HostConfig::validate rather than by the reader.
        ("MINDCLADE_RUNTIME_MAX_CONTROL_BYTES", "1048577"),
        // Model slots may never exceed process slots.
        ("MINDCLADE_RUNTIME_MAX_MODEL_SLOTS", "9"),
        ("MINDCLADE_RUNTIME_MAX_INPUT_BUFFERS", "4097"),
        ("MINDCLADE_RUNTIME_GPU_VENDOR", "not a token"),
    ];
    for (variable, value) in cases {
        let fault = failure(with(variable, value));
        assert_eq!(
            fault.code(),
            Code::InvalidArgument,
            "{variable}={value:?} must be InvalidArgument, got {fault}"
        );
    }

    // The two sockets must differ.
    let same = with(
        "MINDCLADE_RUNTIME_HOST_GRPC_SOCKET",
        "/run/mindclade/host.sock",
    );
    assert_eq!(failure(same).code(), Code::InvalidArgument);

    // The key validity window must be ordered.
    let inverted = with("MINDCLADE_RUNTIME_KEY_NOT_AFTER_MS", "500");
    assert_eq!(failure(inverted).code(), Code::InvalidArgument);
}

#[test]
fn the_preloaded_model_group_is_all_or_nothing() {
    let complete = with_model(valid());
    let config = BootstrapConfig::from_variables(complete.clone()).expect("complete model group");
    let model = config.preloaded_model.expect("preloaded model");
    assert_eq!(model.minimum_gpu_memory_bytes, 8_589_934_592);
    assert_eq!(model.pinned_memory_bytes, 0);

    for variable in [
        "MINDCLADE_RUNTIME_MODEL_BUNDLE_DIGEST",
        "MINDCLADE_RUNTIME_MODEL_WORKER_EXECUTABLE",
        "MINDCLADE_RUNTIME_MODEL_WORKER_SOCKET",
        "MINDCLADE_RUNTIME_MODEL_WORKER_CONFIG",
    ] {
        let mut partial = complete.clone();
        partial.remove(variable);
        let fault =
            BootstrapConfig::from_variables(partial).expect_err("partial group must be rejected");
        assert_eq!(
            fault.code(),
            Code::InvalidArgument,
            "{variable}: a partial model group must be InvalidArgument, got {fault}"
        );
    }

    // An operator-supplied empty value counts as configured, exactly as
    // `env::var(..).ok()` did — so it fails on the value, not by silently
    // disabling the whole group.
    let mut empty_member = complete.clone();
    empty_member.insert(
        "MINDCLADE_RUNTIME_MODEL_WORKER_SOCKET".to_owned(),
        String::new(),
    );
    assert!(BootstrapConfig::from_variables(empty_member).is_err());
}

#[test]
fn model_memory_settings_apply_only_when_the_group_is_configured() {
    // Required once the group is present...
    let mut missing = with_model(valid());
    missing.remove("MINDCLADE_RUNTIME_MODEL_GPU_MEMORY_BYTES");
    assert_eq!(
        BootstrapConfig::from_variables(missing)
            .expect_err("model GPU memory is required with a model")
            .code(),
        Code::FailedPrecondition
    );

    // ...and irrelevant when it is not. This is why they are a second catalog:
    // declaring them optional in the main one would accept the case above.
    let without_model = valid();
    assert!(BootstrapConfig::from_variables(without_model).is_ok());

    let zero = {
        let mut variables = with_model(valid());
        variables.insert(
            "MINDCLADE_RUNTIME_MODEL_GPU_MEMORY_BYTES".to_owned(),
            "0".to_owned(),
        );
        variables
    };
    assert_eq!(
        BootstrapConfig::from_variables(zero)
            .expect_err("zero model GPU memory")
            .code(),
        Code::InvalidArgument
    );

    let pinned = {
        let mut variables = with_model(valid());
        variables.insert(
            "MINDCLADE_RUNTIME_MODEL_PINNED_MEMORY_BYTES".to_owned(),
            "1024".to_owned(),
        );
        variables
    };
    let config = BootstrapConfig::from_variables(pinned).expect("pinned memory");
    assert_eq!(
        config
            .preloaded_model
            .expect("preloaded model")
            .pinned_memory_bytes,
        1024
    );
}

#[test]
fn an_undeclared_key_is_rejected_rather_than_ignored() {
    use mindclade_config::MapSource;

    let source = MapSource::new("file").with("host.sokcet", "/run/mindclade/host.sock");
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
    let rendered = config::catalog().expect("catalog").documentation();
    for (key, _) in config::BINDINGS {
        assert!(rendered.contains(key), "undocumented setting {key}");
    }
    for field in config::catalog().expect("catalog").fields() {
        assert!(
            field.doc().len() > 20,
            "{}: documentation is too thin to be useful",
            field.key()
        );
    }
}
