// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Typed error contracts shared by Mindclade Rust components.
#![forbid(unsafe_code)]
mod code;
mod context;
mod fault;
pub mod retry;
pub mod status;
pub mod wire;
pub use code::Code;
pub use context::{Context, ContextValue};
pub use fault::{Fault, FaultResult, RetryHint};
pub use wire::{WireContext, WireFault, WireRetryKind};
