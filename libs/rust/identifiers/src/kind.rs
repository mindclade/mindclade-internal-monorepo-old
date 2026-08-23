// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! The validated prefix half of a resource identifier.
//!
//! There is one kind grammar and it lives in `resource_id`. This type used to
//! carry a second one — 1 to 48 bytes of `[a-z0-9]` plus `_` and `-` after the
//! first — which admitted values the identifier format cannot carry.
//! `ResourceKind::parse("runtime_host")` succeeded while
//! `ResourceId::parse("runtime_host_<32 hex>")` failed on "more than one
//! separator", and `libs/go/identifiers.ParseKind` and
//! `libs/python/identifiers.parse_kind` both reject `run_id`, `run-id`, `1run`
//! and `a` outright. A validator that accepts what the wire format rejects is a
//! parse failure waiting at the language boundary.

use crate::resource_id::validate_kind;
use mindclade_faults::{Fault, FaultResult};

#[derive(Clone, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub struct ResourceKind(String);

impl ResourceKind {
    pub fn parse(value: impl Into<String>) -> FaultResult<Self> {
        let value = value.into();
        validate_kind(&value).map_err(|error| {
            Fault::invalid_argument("resource kind is invalid").with_source(error)
        })?;
        Ok(Self(value))
    }
    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}
