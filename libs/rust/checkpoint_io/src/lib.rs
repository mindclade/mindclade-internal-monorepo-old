//! Atomic checkpoint publication, bounded staging, verification and repair planning.
#![forbid(unsafe_code)]
mod manifest;
pub mod reader;
pub mod repair;
pub mod staging;
pub mod verify;
mod writer;
pub use manifest::{
    CheckpointManifest, CheckpointShard, CHECKPOINT_SCHEMA
};
pub use reader::{
    CheckpointReader, VerificationReport
};
pub use repair::RepairPlan;
pub use staging::StagingBudget;
pub use writer::{
    CheckpointSession, CheckpointWriter
};
