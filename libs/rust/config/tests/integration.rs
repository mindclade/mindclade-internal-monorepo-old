// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Behavior contract for `mindclade_config`.
//!
//! The cases here mirror `libs/go/config/config_test.go` one for one, because
//! cross-language divergence in how a service reads its own configuration is
//! the defect this crate exists to close.

use mindclade_config::{
    AtomicConfig, Catalog, EnvSource, Field, MAX_VALUE_BYTES, MapSource, Provenance, Secret,
    Snapshot, reason,
};
use mindclade_faults::{Code, Fault};
use std::collections::BTreeMap;

fn reason_of(fault: &Fault) -> String {
    fault
        .context()
        .get("reason")
        .map(ToString::to_string)
        .unwrap_or_default()
}

fn catalog() -> Catalog {
    Catalog::new("example")
        .expect("namespace")
        .declare(Field::required("service.name", "Process identity."))
        .expect("service.name")
        .declare(Field::required("database.dsn", "Durable store DSN.").secret())
        .expect("database.dsn")
        .declare(Field::defaulted("log.level", "Log verbosity.", "info").reloadable())
        .expect("log.level")
}

#[test]
fn merges_sources_in_order_and_records_provenance() {
    let file = MapSource::new("file")
        .with("service.name", "scheduler")
        .with("database.dsn", "postgres://secret");
    let environment = MapSource::new("environment").with("log.level", "debug");
    let snapshot = catalog().load(&[&file, &environment]).expect("load");

    assert_eq!(snapshot.raw("log.level").expect("log.level"), "debug");
    assert_eq!(
        snapshot.origin("log.level").expect("origin").source(),
        "environment"
    );
    assert_eq!(
        snapshot.origin("service.name").expect("origin").source(),
        "file"
    );
    assert!(snapshot.is_set("service.name").expect("is_set"));
}

#[test]
fn unresolved_field_falls_back_to_its_default_with_default_provenance() {
    let file = MapSource::new("file")
        .with("service.name", "scheduler")
        .with("database.dsn", "postgres://secret");
    let snapshot = catalog().load(&[&file]).expect("load");

    assert_eq!(snapshot.raw("log.level").expect("log.level"), "info");
    let origin = snapshot.origin("log.level").expect("origin");
    assert_eq!(origin.provenance(), Provenance::Default);
    assert_eq!(origin.source(), "default");
    assert!(!snapshot.is_set("log.level").expect("is_set"));
}

#[test]
fn unknown_key_is_rejected() {
    let source = MapSource::new("file")
        .with("service.name", "scheduler")
        .with("database.dsn", "postgres://secret")
        .with("service.nmae", "typo");
    let fault = catalog().load(&[&source]).expect_err("unknown key");

    assert_eq!(fault.code(), Code::InvalidArgument);
    assert_eq!(reason_of(&fault), reason::KEY_UNKNOWN);
}

#[test]
fn missing_required_value_is_rejected() {
    let source = MapSource::new("file").with("service.name", "scheduler");
    let fault = catalog().load(&[&source]).expect_err("missing required");

    assert_eq!(fault.code(), Code::FailedPrecondition);
    assert_eq!(reason_of(&fault), reason::VALUE_MISSING);
}

#[test]
fn secret_never_appears_in_debug_display_redaction_or_digest() {
    const PLAINTEXT: &str = "postgres://user:hunter2@db/app";
    let source = MapSource::new("file")
        .with("service.name", "scheduler")
        .with("database.dsn", PLAINTEXT);
    let snapshot = catalog().load(&[&source]).expect("load");

    let secret: Secret = snapshot.secret("database.dsn").expect("secret");
    assert_eq!(secret.expose(), PLAINTEXT);
    assert_eq!(format!("{secret:?}"), "[REDACTED]");
    assert_eq!(format!("{secret}"), "[REDACTED]");

    let rendered = format!("{snapshot:?}");
    assert!(!rendered.contains("hunter2"), "Debug leaked: {rendered}");
    assert!(rendered.contains("[REDACTED]"));
    assert_eq!(snapshot.redacted()["database.dsn"], "[REDACTED]");
    assert!(!format!("{:?}", snapshot.redacted()).contains("hunter2"));

    // The digest commits to the secret without carrying it: rotating the value
    // moves the digest, and neither the digest nor its rendering holds bytes of
    // the plaintext.
    let digest = snapshot.digest().to_hex();
    assert!(!digest.contains("hunter2"));
    let rotated = catalog()
        .load(&[&MapSource::new("file")
            .with("service.name", "scheduler")
            .with("database.dsn", "postgres://user:rotated@db/app")])
        .expect("load");
    assert_ne!(snapshot.digest().to_hex(), rotated.digest().to_hex());
}

#[test]
fn secret_cannot_be_read_as_a_plain_string() {
    let source = MapSource::new("file")
        .with("service.name", "scheduler")
        .with("database.dsn", "postgres://secret");
    let snapshot = catalog().load(&[&source]).expect("load");

    let fault = snapshot.raw("database.dsn").expect_err("secret via raw");
    assert_eq!(fault.code(), Code::InvalidArgument);
    assert!(snapshot.string("database.dsn").is_err());
    assert!(!snapshot.redacted()["database.dsn"].contains("postgres"));
}

#[test]
fn digest_is_deterministic_and_frames_values_unambiguously() {
    let first = catalog()
        .load(&[&MapSource::new("file")
            .with("service.name", "a")
            .with("database.dsn", "bc")])
        .expect("load");
    let second = catalog()
        .load(&[&MapSource::new("file")
            .with("service.name", "a")
            .with("database.dsn", "bc")])
        .expect("load");
    assert!(first.equivalent(&second));

    // Length-prefixed framing: shifting a byte across a value boundary must not
    // produce the same digest.
    let shifted = catalog()
        .load(&[&MapSource::new("file")
            .with("service.name", "ab")
            .with("database.dsn", "c")])
        .expect("load");
    assert!(!first.equivalent(&shifted));
}

#[test]
fn digest_tracks_provenance_not_just_values() {
    let from_file = catalog()
        .load(&[&MapSource::new("file")
            .with("service.name", "a")
            .with("database.dsn", "b")
            .with("log.level", "info")])
        .expect("load");
    let from_default = catalog()
        .load(&[&MapSource::new("file")
            .with("service.name", "a")
            .with("database.dsn", "b")])
        .expect("load");

    assert_eq!(
        from_file.raw("log.level").expect("value"),
        from_default.raw("log.level").expect("value")
    );
    assert!(!from_file.equivalent(&from_default));
}

#[test]
fn reload_rejects_a_change_to_a_field_that_is_not_reloadable() {
    let base = |name: &str, level: &str| -> Snapshot {
        catalog()
            .load(&[&MapSource::new("file")
                .with("service.name", name)
                .with("database.dsn", "postgres://secret")
                .with("log.level", level)])
            .expect("load")
    };
    let atomic = AtomicConfig::new(base("api", "info"));

    let fault = atomic
        .apply(base("other", "debug"))
        .expect_err("restart required");
    assert_eq!(fault.code(), Code::FailedPrecondition);
    assert_eq!(reason_of(&fault), reason::RESTART_REQUIRED);
    // Last known good is retained.
    assert_eq!(
        atomic
            .snapshot()
            .expect("snapshot")
            .raw("service.name")
            .expect("value"),
        "api"
    );

    atomic
        .apply(base("api", "debug"))
        .expect("reloadable change");
    assert_eq!(
        atomic
            .snapshot()
            .expect("snapshot")
            .raw("log.level")
            .expect("value"),
        "debug"
    );
}

#[test]
fn env_source_reads_only_bound_variables() {
    let environment = EnvSource::from_table(BTreeMap::from([
        ("EXAMPLE_NAME".to_owned(), "scheduler".to_owned()),
        ("EXAMPLE_DSN".to_owned(), "postgres://secret".to_owned()),
        // Present in the environment, bound to nothing. A scanning loader would
        // capture it and then reject it as an unknown key; this one never sees
        // it, which is what makes unknown-key rejection usable at all.
        ("EXAMPLE_UNRELATED".to_owned(), "ignored".to_owned()),
    ]))
    .bind("service.name", "EXAMPLE_NAME")
    .bind("database.dsn", "EXAMPLE_DSN");

    let snapshot = catalog().load(&[&environment]).expect("load");
    assert_eq!(snapshot.raw("service.name").expect("value"), "scheduler");
    assert_eq!(snapshot.keys().count(), 3);
}

#[test]
fn missing_required_value_names_the_environment_variable() {
    let environment = EnvSource::from_table(BTreeMap::new())
        .bind("service.name", "EXAMPLE_NAME")
        .bind("database.dsn", "EXAMPLE_DSN");
    let fault = catalog().load(&[&environment]).expect_err("missing");

    assert_eq!(
        fault.context().get("variable").map(ToString::to_string),
        Some("EXAMPLE_DSN".to_owned())
    );
}

#[test]
fn field_documentation_is_mandatory() {
    let fault = Catalog::new("example")
        .expect("namespace")
        .declare(Field::required("service.name", "   "))
        .expect_err("blank documentation");
    assert_eq!(reason_of(&fault), reason::FIELD_INVALID);
}

#[test]
fn documentation_renders_the_whole_surface() {
    let rendered = catalog().documentation();
    for key in ["service.name", "database.dsn", "log.level"] {
        assert!(rendered.contains(key), "missing {key} in:\n{rendered}");
    }
    assert!(rendered.contains("required"));
    assert!(rendered.contains("default `info`"));
    assert!(rendered.contains("Durable store DSN."));
    // The rendered surface is documentation, not a value dump: it names the
    // secret field without carrying a value for it.
    assert!(!rendered.contains("postgres"));
}

#[test]
fn duplicate_and_non_canonical_declarations_are_rejected() {
    assert!(
        Catalog::new("example")
            .expect("namespace")
            .declare(Field::required("service.name", "One."))
            .expect("first")
            .declare(Field::required("service.name", "Two."))
            .is_err()
    );
    for key in ["Service.Name", "", ".leading", " padded", "sérvice"] {
        assert!(
            Catalog::new("example")
                .expect("namespace")
                .declare(Field::required(key, "Doc."))
                .is_err(),
            "accepted non-canonical key {key:?}"
        );
    }
}

#[test]
fn values_are_bounded() {
    let catalog = Catalog::new("example")
        .expect("namespace")
        .declare(Field::required("service.name", "Identity.").maximum_bytes(8))
        .expect("field");
    let fault = catalog
        .load(&[&MapSource::new("file").with("service.name", "0123456789")])
        .expect_err("over ceiling");
    assert_eq!(reason_of(&fault), reason::VALUE_INVALID);

    assert!(
        Catalog::new("example")
            .expect("namespace")
            .declare(
                Field::required("service.name", "Identity.").maximum_bytes(MAX_VALUE_BYTES + 1)
            )
            .is_err()
    );
}

#[test]
fn empty_value_policy_is_declared_per_field() {
    let catalog = Catalog::new("example")
        .expect("namespace")
        .declare(Field::defaulted(
            "verbatim.key",
            "Kept as supplied.",
            "fallback",
        ))
        .expect("verbatim")
        .declare(Field::defaulted("default.key", "Empty means unset.", "0").empty_uses_default())
        .expect("default");
    let snapshot = catalog
        .load(&[&MapSource::new("file")
            .with("verbatim.key", "")
            .with("default.key", "")])
        .expect("load");

    assert_eq!(snapshot.raw("verbatim.key").expect("value"), "");
    assert!(snapshot.is_set("verbatim.key").expect("is_set"));
    assert_eq!(snapshot.raw("default.key").expect("value"), "0");
    assert!(!snapshot.is_set("default.key").expect("is_set"));
}

#[test]
fn whitespace_policy_is_declared_per_field() {
    let strict = Catalog::new("example")
        .expect("namespace")
        .declare(Field::required("strict.key", "Trimmed exactly.").reject_surrounding_whitespace())
        .expect("field");
    assert!(
        strict
            .load(&[&MapSource::new("file").with("strict.key", " padded ")])
            .is_err()
    );

    let lenient = Catalog::new("example")
        .expect("namespace")
        .declare(Field::required("lenient.key", "Preserved verbatim."))
        .expect("field");
    assert_eq!(
        lenient
            .load(&[&MapSource::new("file").with("lenient.key", " padded ")])
            .expect("load")
            .raw("lenient.key")
            .expect("value"),
        " padded "
    );

    let blank = Catalog::new("example")
        .expect("namespace")
        .declare(Field::required("blank.key", "Blank counts as absent.").blank_is_missing())
        .expect("field");
    let fault = blank
        .load(&[&MapSource::new("file").with("blank.key", "   ")])
        .expect_err("blank");
    assert_eq!(fault.code(), Code::FailedPrecondition);
    assert_eq!(reason_of(&fault), reason::VALUE_MISSING);
}

#[test]
fn typed_accessors_report_the_documented_codes() {
    let catalog = Catalog::new("example")
        .expect("namespace")
        .declare(Field::required("count", "A count."))
        .expect("field");
    let load = |value: &str| -> Snapshot {
        catalog
            .load(&[&MapSource::new("file").with("count", value)])
            .expect("load")
    };

    assert_eq!(load("7").u64_positive("count").expect("value"), 7);
    assert_eq!(
        load("0").u64_positive("count").expect_err("zero").code(),
        Code::InvalidArgument
    );
    assert_eq!(
        load("x").u64("count").expect_err("parse").code(),
        Code::InvalidArgument
    );
    // Width overflow is out-of-range, not invalid-argument: the value parsed.
    assert_eq!(
        load("5000000000")
            .u32_positive("count")
            .expect_err("width")
            .code(),
        Code::OutOfRange
    );
    assert_eq!(
        load("11")
            .u64_bounded("count", 1, 10)
            .expect_err("bounds")
            .code(),
        Code::InvalidArgument
    );
    assert!(load("/etc/mindclade").absolute_path("count").is_ok());
    assert!(load("relative/path").absolute_path("count").is_err());
    assert!(
        load("/etc/../etc")
            .resolved_absolute_path("count", 1024)
            .is_err()
    );
    assert!(load("true").equals("count", "true").expect("equals"));
    assert!(!load("TRUE").equals("count", "true").expect("equals"));
}
