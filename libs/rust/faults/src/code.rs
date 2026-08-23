// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Stable machine-readable error codes.
//!
//! The set mirrors `mindclade.common.v1.ErrorCode` in
//! `protocols/proto/mindclade/common/v1/errors.proto`, which is the wire
//! authority, and `libs/go/faults`, which is the other mirror.
//! `tests/integration/cross_language/test_error_codes.py` fails if the three
//! drift apart.

use core::fmt;
use core::str::FromStr;

/// Longest canonical spelling is `failed_precondition` at 19 bytes. Parsing is
/// an ingestion path fed by peers, so it rejects on length before it allocates
/// a normalized copy rather than lowercasing an arbitrarily long peer string.
const MAXIMUM_CODE_BYTES: usize = 32;

/// Stable classes used across process and protocol boundaries.
///
/// Deliberately **not** `#[non_exhaustive]`. Every crate in this workspace is
/// `publish = false`, so the attribute bought no external-API stability; what
/// it bought instead was a compulsory `_ =>` arm in every downstream `match`,
/// and four transport edges used that arm to render `NotFound`, `Aborted`,
/// `Unimplemented`, and `DataLoss` as HTTP 500 / gRPC `internal`. Exhaustive
/// matching is the mechanism that keeps a code added here from silently
/// inheriting a catch-all at an edge that has not been taught about it.
///
/// Totality on ingestion — the reason a taxonomy usually reaches for the
/// attribute — is provided at the *value* level instead, by [`Code::Unknown`]
/// and [`Code::from_wire`]: a peer's unrecognized code still parses, it just
/// parses into a code every match already names.
#[derive(Clone, Copy, Debug, Eq, Hash, Ord, PartialEq, PartialOrd)]
pub enum Code {
    /// Classification failed, or a peer sent a code this build does not know.
    ///
    /// This is the landing spot that keeps ingestion total. Without it a peer's
    /// own catch-all — `libs/go/faults` emits `"unknown"` exactly when it cannot
    /// classify — was a code this crate could not parse at all.
    Unknown,
    InvalidArgument,
    NotFound,
    AlreadyExists,
    FailedPrecondition,
    Aborted,
    OutOfRange,
    Unimplemented,
    Internal,
    Unavailable,
    DataLoss,
    Unauthenticated,
    PermissionDenied,
    ResourceExhausted,
    DeadlineExceeded,
    Cancelled,
    Conflict,
}

impl Code {
    /// Returns the stable snake-case wire representation.
    ///
    /// `Cancelled` and `Unimplemented` serialize as `canceled` and
    /// `not_implemented`: those are the spellings already on
    /// `Mindclade-Error-Code` headers and in telemetry attributes emitted by the
    /// Go control plane, so they are the canonical ones. The variant names stay
    /// as they are because they mirror the tonic/gRPC names used at this
    /// crate's own transport boundary.
    #[must_use]
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Unknown => "unknown",
            Self::InvalidArgument => "invalid_argument",
            Self::NotFound => "not_found",
            Self::AlreadyExists => "already_exists",
            Self::FailedPrecondition => "failed_precondition",
            Self::Aborted => "aborted",
            Self::OutOfRange => "out_of_range",
            Self::Unimplemented => "not_implemented",
            Self::Internal => "internal",
            Self::Unavailable => "unavailable",
            Self::DataLoss => "data_loss",
            Self::Unauthenticated => "unauthenticated",
            Self::PermissionDenied => "permission_denied",
            Self::ResourceExhausted => "resource_exhausted",
            Self::DeadlineExceeded => "deadline_exceeded",
            Self::Cancelled => "canceled",
            Self::Conflict => "conflict",
        }
    }
    /// Parses a wire code, degrading anything unrecognized to [`Code::Unknown`].
    ///
    /// Use this wherever the input comes from a peer. [`FromStr`] stays strict
    /// so configuration and test vectors still fail loudly on a typo, but a
    /// remote fault must never be rejected because the sender runs a build that
    /// knows one more code than this one does.
    #[must_use]
    pub fn from_wire(value: &str) -> Self {
        Self::from_str(value).unwrap_or(Self::Unknown)
    }
    /// Whether failures with this code are commonly transient.
    #[must_use]
    pub const fn is_transient(self) -> bool {
        matches!(
            self,
            Self::Aborted
                | Self::Unavailable
                | Self::ResourceExhausted
                | Self::DeadlineExceeded
                | Self::Conflict
        )
    }
}

impl fmt::Display for Code {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(self.as_str())
    }
}

/// Returned when an unknown wire code is parsed.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ParseCodeError(String);

impl fmt::Display for ParseCodeError {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "unknown error code: {}", self.0)
    }
}

impl std::error::Error for ParseCodeError {}

impl FromStr for Code {
    type Err = ParseCodeError;
    fn from_str(value: &str) -> Result<Self, Self::Err> {
        let trimmed = value.trim();
        if trimmed.len() > MAXIMUM_CODE_BYTES {
            // Echo a bounded prefix; the rejected value came from a peer and the
            // error text reaches logs.
            let mut end = MAXIMUM_CODE_BYTES;
            while end > 0 && !trimmed.is_char_boundary(end) {
                end -= 1;
            }
            return Err(ParseCodeError(trimmed[..end].to_owned()));
        }
        // Case is normalized to match `faults.ParseCode` on the Go side, so the
        // same input resolves the same way in both mirrors.
        let normalized = trimmed.to_ascii_lowercase();
        // The second spelling in each pair is the one this crate emitted before
        // the taxonomy was reconciled. Both mirrors accept both spellings and
        // emit one, so values already in flight keep resolving.
        let code = match normalized.as_str() {
            "unknown" => Self::Unknown,
            "invalid_argument" => Self::InvalidArgument,
            "not_found" => Self::NotFound,
            "already_exists" => Self::AlreadyExists,
            "failed_precondition" => Self::FailedPrecondition,
            "aborted" => Self::Aborted,
            "out_of_range" => Self::OutOfRange,
            "not_implemented" | "unimplemented" => Self::Unimplemented,
            "internal" => Self::Internal,
            "unavailable" => Self::Unavailable,
            "data_loss" => Self::DataLoss,
            "unauthenticated" => Self::Unauthenticated,
            "permission_denied" => Self::PermissionDenied,
            "resource_exhausted" => Self::ResourceExhausted,
            "deadline_exceeded" => Self::DeadlineExceeded,
            "canceled" | "cancelled" => Self::Cancelled,
            "conflict" => Self::Conflict,
            _ => return Err(ParseCodeError(normalized)),
        };
        Ok(code)
    }
}
