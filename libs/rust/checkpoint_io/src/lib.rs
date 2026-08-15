// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Atomic checkpoint publication, bounded staging, verification and repair planning.
#![forbid(unsafe_code)]
mod manifest;
pub mod reader;
pub mod repair;
pub mod staging;
pub mod verify;
mod writer;
pub use manifest::{CHECKPOINT_SCHEMA, CheckpointManifest, CheckpointShard};
pub use reader::{CheckpointReader, VerificationReport};
pub use repair::RepairPlan;
pub use staging::StagingBudget;
pub use writer::{CheckpointSession, CheckpointWriter};
