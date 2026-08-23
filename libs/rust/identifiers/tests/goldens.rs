// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Rust's seat at the three-language identifier table.
//!
//! The kind vectors below are the same ones `libs/go/identifiers/kind_test.go`
//! and `libs/python/identifiers/tests/test_resource.py` run, and
//! `tests/integration/cross_language/test_identifiers.py` carries them a third
//! time so a language that drifts fails there too.

use mindclade_identifiers::{
    ID_BODY_LENGTH, ID_SEPARATOR, MAXIMUM_KIND_LENGTH, MINIMUM_KIND_LENGTH, ResourceId,
    ResourceKind,
};
use std::str::FromStr;

/// A canonical RFC-variant version 7 UUID body, used to build IDs from a kind.
const BODY: &str = "019c0000000070008000000000000001";

/// Kinds every language accepts.
const ACCEPTED: &[&str] = &["ab", "run", "org", "model2", "runtimehost", "a1b2c3"];

/// Kinds every language rejects. `run_id` and `run-id` are the ones that
/// mattered: this crate used to accept both, and `runtime_host` besides.
const REJECTED: &[&str] = &[
    "",
    "a",
    "1run",
    "Run",
    "run_id",
    "run-id",
    "runtime_host",
    " run",
    "run.id",
    "rūn",
];

#[test]
fn resource_id_is_canonical() {
    let id = ResourceId::from_str(
        "run_01arZ3NDEKTSV4RRFFQ69G5FAV"
            .to_ascii_lowercase()
            .as_str(),
    );
    assert!(
        id.is_err(),
        "fixture deliberately rejects noncanonical mixed schema"
    );
}

#[test]
fn kind_grammar_matches_go_and_python() {
    assert_eq!(MINIMUM_KIND_LENGTH, 2);
    assert_eq!(MAXIMUM_KIND_LENGTH, 24);
    assert_eq!(ID_BODY_LENGTH, 32);
    assert_eq!(ID_SEPARATOR, '_');

    for value in ACCEPTED {
        assert!(
            ResourceKind::parse(*value).is_ok(),
            "kind {value:?} is accepted by Go and Python and must be accepted here"
        );
    }
    for value in REJECTED {
        assert!(
            ResourceKind::parse(*value).is_err(),
            "kind {value:?} is rejected by Go and Python and must be rejected here"
        );
    }

    let longest = "a".repeat(MAXIMUM_KIND_LENGTH);
    assert!(ResourceKind::parse(longest.as_str()).is_ok());
    let too_long = "a".repeat(MAXIMUM_KIND_LENGTH + 1);
    assert!(ResourceKind::parse(too_long.as_str()).is_err());
}

#[test]
fn resource_kind_and_resource_id_share_one_grammar() {
    // The defect this pins: `ResourceKind` carried its own, laxer grammar, so it
    // admitted kinds — `runtime_host` above all — whose identifiers this crate's
    // own parser, `libs/go/identifiers.ParseID` and
    // `libs/python/identifiers.ResourceId.parse` all reject. Every kind either
    // validator accepts must round-trip through the identifier it prefixes.
    for value in ACCEPTED.iter().chain(REJECTED) {
        let accepted_as_kind = ResourceKind::parse(*value).is_ok();
        let text = format!("{value}{ID_SEPARATOR}{BODY}");
        let accepted_in_id = ResourceId::parse(&text).is_ok();
        assert_eq!(
            accepted_as_kind, accepted_in_id,
            "ResourceKind and ResourceId disagree about {value:?}: \
             kind={accepted_as_kind}, id={accepted_in_id}"
        );
        if accepted_in_id {
            let id = ResourceId::parse(&text).expect("checked above");
            assert_eq!(id.kind(), *value);
            assert_eq!(id.body(), BODY);
            assert_eq!(id.to_string(), text);
        }
    }
}
