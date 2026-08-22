// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Rust projections of canonical Mindclade protobuf contracts.
//!
//! Runtime and inference retain their established checked-in projections, while
//! the checkpoint contract is generated from its canonical protobuf sources by
//! this crate's build script. Cross-language qualification freezes the canonical
//! bytes shared with Python and TypeScript.
#![forbid(unsafe_code)]

pub mod artifact;
pub mod common;
pub mod inference;
pub mod registry;
pub mod runtime;
pub mod training;
