// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Shared node execution substrate for ticketed ingestion, preprocessing,
//! checkpoint, artifact, evaluation, and batch stages.
#![forbid(unsafe_code)]

pub mod artifact_transfer;
pub mod bootstrap;
pub mod checkpoint_transfer;
pub mod config;
pub mod data_stream;
pub mod dependencies;
pub mod diagnostics;
pub mod diagnostics_bundle;
pub mod health;
pub mod lifecycle;
pub mod operations;
pub mod process_supervisor;
pub mod provider_store;
pub mod reference_cache;
pub mod resource_monitor;
pub mod server;
pub mod stage_adapters;
pub mod telemetry;
pub mod tool_runner;
pub use checkpoint_transfer::CheckpointTransfer;
pub use config::NodeAgentConfig;
pub use data_stream::StreamWorker;
pub use diagnostics::{DiagnosticBundle, DiagnosticEntry};
pub use diagnostics_bundle::{
    DEFAULT_MAXIMUM_DIAGNOSTIC_BYTES, DiagnosticProcessExit, NodeDiagnosticsBundle,
};
pub use health::{NodeHealth, NodeHealthSnapshot};
pub use dependencies::ObjectStoreProbe;
pub use lifecycle::{ArtifactStoreComponent, NodeAgentComponent};
pub use process_supervisor::{ManagedProcess, ProcessSupervisor};
pub use provider_store::ProviderSource;
pub use reference_cache::{ReferenceCache, ReferenceSnapshot};
pub use resource_monitor::ResourceMonitor;
pub use server::{NodeAgentCore, StageContext, StageExecutor, StageFuture, StageResult};
pub use stage_adapters::{
    CHECKPOINT_COPY_OPERATION, CheckpointCopyExecutor, DATA_STREAM_FETCH_OPERATION,
    DataStreamFetchExecutor,
};
pub use telemetry::NodeMetrics;
pub use tool_runner::{
    MAXIMUM_TOOL_OUTPUT_BYTES, MAXIMUM_TOOL_TIMEOUT, ToolOutput, ToolRequest, ToolRunner,
};
