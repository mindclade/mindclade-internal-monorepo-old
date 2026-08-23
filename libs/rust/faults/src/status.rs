// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Canonical rendering of [`Code`] onto HTTP and gRPC status codes.
//!
//! This is a mirror, not a new opinion. `libs/go/httpx/codes.go`
//! (`StatusFromCode`) and `libs/go/grpcx/codes.go` (`CodeFromFault`) already
//! render the shared taxonomy for the Go control plane, and Rust had no
//! equivalent — every Rust transport edge wrote its own `match` instead. Two of
//! them disagreed: `services/runtime_gateway` rendered `NotFound` as 500 while
//! `services/ai_gateway_proxy` rendered it as 404, so the same fault told one
//! client "your request was wrong" and another "we broke, retry and page an
//! operator". `tests/integration/cross_language/test_fault_status_mapping.py`
//! compares this table against the two Go ones and fails if they drift.
//!
//! The functions return `u16` and `i32` rather than `http::StatusCode` and
//! `tonic::Code` on purpose. No crate under `libs/rust` depends on a transport
//! crate, and a status table is not a reason to make the layer-0 fault crate
//! the first: both values are the numbers the respective specifications
//! define — RFC 9110 for HTTP, the gRPC status code table for gRPC — so the
//! typed conversion belongs at the edge that already owns the transport type.
//!
//! Neither function has a wildcard arm. That is the property that matters: with
//! `Code` no longer `#[non_exhaustive]`, a code added to the taxonomy fails to
//! compile here instead of silently inheriting a catch-all's 500.

use crate::Code;

// gRPC status codes are a fixed numeric table shared by every implementation
// (`grpc/codes` in Go, `tonic::Code` in Rust); the numbers are the contract, so
// they are named here rather than restated as literals inside the match.
const GRPC_CANCELLED: i32 = 1;
const GRPC_UNKNOWN: i32 = 2;
const GRPC_INVALID_ARGUMENT: i32 = 3;
const GRPC_DEADLINE_EXCEEDED: i32 = 4;
const GRPC_NOT_FOUND: i32 = 5;
const GRPC_ALREADY_EXISTS: i32 = 6;
const GRPC_PERMISSION_DENIED: i32 = 7;
const GRPC_RESOURCE_EXHAUSTED: i32 = 8;
const GRPC_FAILED_PRECONDITION: i32 = 9;
const GRPC_ABORTED: i32 = 10;
const GRPC_OUT_OF_RANGE: i32 = 11;
const GRPC_UNIMPLEMENTED: i32 = 12;
const GRPC_INTERNAL: i32 = 13;
const GRPC_UNAVAILABLE: i32 = 14;
const GRPC_DATA_LOSS: i32 = 15;
const GRPC_UNAUTHENTICATED: i32 = 16;

/// Renders a fault code as the HTTP status a client sees.
///
/// Mirrors `httpx.StatusFromCode`. Only `Internal`, `DataLoss`, and `Unknown`
/// render as 500: a 500 asserts the server failed, which tells a client to
/// retry and tells an operator to look. Every code that describes something
/// about the *request* renders in the 4xx range so neither signal fires for a
/// caller's own mistake.
#[must_use]
pub const fn http_status(code: Code) -> u16 {
    match code {
        // 408 rather than 499: the caller aborted, and 499 is an nginx
        // extension no HTTP specification defines. `httpx.StatusFromCode` chose
        // 408 and `httpx.CodeFromStatus` maps 408 back to `Canceled`, so this
        // is the spelling that round-trips through the Go edge.
        Code::Cancelled => 408,
        Code::InvalidArgument | Code::OutOfRange => 400,
        Code::Unauthenticated => 401,
        Code::PermissionDenied => 403,
        Code::NotFound => 404,
        // 409 covers all three: the request conflicts with the resource's
        // current state and a caller may resolve it and retry.
        Code::AlreadyExists | Code::Conflict | Code::Aborted => 409,
        // 412, not 409. A precondition failure says the caller's stated
        // expectation about the resource no longer holds, which is the exact
        // meaning of 412 and is separately actionable from a state conflict.
        Code::FailedPrecondition => 412,
        Code::ResourceExhausted => 429,
        Code::Unimplemented => 501,
        Code::Unavailable => 503,
        Code::DeadlineExceeded => 504,
        Code::Internal | Code::DataLoss | Code::Unknown => 500,
    }
}

/// Renders a fault code as the numeric gRPC status a client sees.
///
/// Mirrors `grpcx.CodeFromFault`. The value is the canonical gRPC status code
/// number; convert it at the edge, where `tonic::Code::from(i32)` is total.
///
/// gRPC's `Ok` has no counterpart by construction: a [`Code`] only ever
/// describes a failure, so this never returns 0.
#[must_use]
pub const fn grpc_code(code: Code) -> i32 {
    match code {
        Code::Cancelled => GRPC_CANCELLED,
        Code::Unknown => GRPC_UNKNOWN,
        Code::InvalidArgument => GRPC_INVALID_ARGUMENT,
        Code::DeadlineExceeded => GRPC_DEADLINE_EXCEEDED,
        Code::NotFound => GRPC_NOT_FOUND,
        Code::AlreadyExists => GRPC_ALREADY_EXISTS,
        Code::PermissionDenied => GRPC_PERMISSION_DENIED,
        Code::ResourceExhausted => GRPC_RESOURCE_EXHAUSTED,
        Code::FailedPrecondition => GRPC_FAILED_PRECONDITION,
        // gRPC has no `Conflict`; `Aborted` is its concurrency-conflict code
        // and is what `grpcx.CodeFromFault` picks for both.
        Code::Conflict | Code::Aborted => GRPC_ABORTED,
        Code::OutOfRange => GRPC_OUT_OF_RANGE,
        Code::Unimplemented => GRPC_UNIMPLEMENTED,
        Code::Internal => GRPC_INTERNAL,
        Code::Unavailable => GRPC_UNAVAILABLE,
        Code::DataLoss => GRPC_DATA_LOSS,
        Code::Unauthenticated => GRPC_UNAUTHENTICATED,
    }
}

/// Every code in the taxonomy, in declaration order.
///
/// Exists so callers and conformance tests can iterate the mapping. It is
/// hand-written, so `code_slice_covers_every_variant` guards it with a
/// wildcard-free match that stops compiling when a variant is added.
pub const ALL: &[Code] = &[
    Code::Unknown,
    Code::InvalidArgument,
    Code::NotFound,
    Code::AlreadyExists,
    Code::FailedPrecondition,
    Code::Aborted,
    Code::OutOfRange,
    Code::Unimplemented,
    Code::Internal,
    Code::Unavailable,
    Code::DataLoss,
    Code::Unauthenticated,
    Code::PermissionDenied,
    Code::ResourceExhausted,
    Code::DeadlineExceeded,
    Code::Cancelled,
    Code::Conflict,
];

#[cfg(test)]
mod tests {
    use super::{ALL, Code, grpc_code, http_status};

    /// `ALL` is hand-written, so it needs its own guard. The `match` is
    /// exhaustive and wildcard-free: a new variant fails to compile here, which
    /// is the signal to extend the slice.
    #[test]
    fn code_slice_covers_every_variant() {
        for code in ALL {
            let named = match code {
                Code::Unknown
                | Code::InvalidArgument
                | Code::NotFound
                | Code::AlreadyExists
                | Code::FailedPrecondition
                | Code::Aborted
                | Code::OutOfRange
                | Code::Unimplemented
                | Code::Internal
                | Code::Unavailable
                | Code::DataLoss
                | Code::Unauthenticated
                | Code::PermissionDenied
                | Code::ResourceExhausted
                | Code::DeadlineExceeded
                | Code::Cancelled
                | Code::Conflict => true,
            };
            assert!(named, "{code} is not named");
        }
        let mut sorted: Vec<Code> = ALL.to_vec();
        sorted.sort_unstable();
        sorted.dedup();
        assert_eq!(sorted.len(), ALL.len(), "ALL repeats a code");
        assert_eq!(sorted.len(), 17, "the taxonomy is 17 codes");
    }

    #[test]
    fn every_code_renders_a_usable_http_status() {
        for &code in ALL {
            let status = http_status(code);
            assert!(
                (400..600).contains(&status),
                "{code} renders HTTP {status}, which is not an error status"
            );
        }
    }

    /// The regression this module exists for. A fault that describes the
    /// request must not tell the client to retry and page an operator.
    #[test]
    fn only_server_side_faults_render_500() {
        for &code in ALL {
            let is_server_fault = matches!(code, Code::Internal | Code::DataLoss | Code::Unknown);
            assert_eq!(
                http_status(code) == 500,
                is_server_fault,
                "{code} renders HTTP {}",
                http_status(code)
            );
        }
    }

    #[test]
    fn not_found_is_not_an_availability_signal() {
        assert_eq!(http_status(Code::NotFound), 404);
        assert_eq!(grpc_code(Code::NotFound), 5);
    }

    #[test]
    fn every_code_renders_a_failing_grpc_code() {
        for &code in ALL {
            let rendered = grpc_code(code);
            assert!(
                (1..=16).contains(&rendered),
                "{code} renders gRPC {rendered}, which is not a failure code"
            );
        }
        // `Conflict` and `Aborted` share `Aborted`; every other code is
        // distinct, so collapsing the set loses exactly one entry.
        let mut rendered: Vec<i32> = ALL.iter().map(|&code| grpc_code(code)).collect();
        rendered.sort_unstable();
        rendered.dedup();
        assert_eq!(rendered.len(), ALL.len() - 1);
    }

    /// Pins the full table. The cross-language test compares it against Go;
    /// this one fails locally the moment a value moves.
    #[test]
    fn the_table_is_what_it_says_it_is() {
        let expected: &[(Code, u16, i32)] = &[
            (Code::Unknown, 500, 2),
            (Code::InvalidArgument, 400, 3),
            (Code::NotFound, 404, 5),
            (Code::AlreadyExists, 409, 6),
            (Code::FailedPrecondition, 412, 9),
            (Code::Aborted, 409, 10),
            (Code::OutOfRange, 400, 11),
            (Code::Unimplemented, 501, 12),
            (Code::Internal, 500, 13),
            (Code::Unavailable, 503, 14),
            (Code::DataLoss, 500, 15),
            (Code::Unauthenticated, 401, 16),
            (Code::PermissionDenied, 403, 7),
            (Code::ResourceExhausted, 429, 8),
            (Code::DeadlineExceeded, 504, 4),
            (Code::Cancelled, 408, 1),
            (Code::Conflict, 409, 10),
        ];
        assert_eq!(expected.len(), ALL.len());
        for &(code, http, grpc) in expected {
            assert_eq!(http_status(code), http, "HTTP status for {code}");
            assert_eq!(grpc_code(code), grpc, "gRPC code for {code}");
        }
    }
}
