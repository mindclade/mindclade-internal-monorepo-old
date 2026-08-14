//! Committed Rust projections of canonical Mindclade protobuf contracts.
//!
//! Bazel owns canonical protobuf code generation.  This crate keeps a checked-in
//! projection for Cargo-native qualification and local runtime leaves.  The
//! cross-language qualification lane proves these layouts stay compatible with
//! `protocols/proto/mindclade/runtime/v1`.
#![forbid(unsafe_code)]

pub mod runtime;
