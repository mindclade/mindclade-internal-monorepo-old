// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

//! Ticket-validated ingestion worker primitives.
#![forbid(unsafe_code)]

mod config;
mod executor;
mod lifecycle;

pub use config::IngestionWorkerConfig;
pub use executor::{IngestionEngine, IngestionExecutor};
pub use lifecycle::Lifecycle;
