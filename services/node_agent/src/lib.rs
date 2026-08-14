//! Shared node execution substrate for ticketed ingestion, preprocessing,
//! checkpoint, artifact, evaluation, and batch stages.
#![forbid(unsafe_code)]

pub mod artifact_transfer;
pub mod checkpoint_transfer;
pub mod config;
pub mod data_stream;
pub mod diagnostics;
pub mod diagnostics_bundle;
pub mod health;
pub mod lifecycle;
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
    DiagnosticProcessExit, NodeDiagnosticsBundle, DEFAULT_MAXIMUM_DIAGNOSTIC_BYTES,
};
pub use health::{NodeHealth, NodeHealthSnapshot};
pub use lifecycle::NodeAgentComponent;
pub use process_supervisor::{ManagedProcess, ProcessSupervisor};
pub use provider_store::ProviderSource;
pub use reference_cache::{ReferenceCache, ReferenceSnapshot};
pub use resource_monitor::ResourceMonitor;
pub use server::{NodeAgentCore, StageContext, StageExecutor, StageFuture, StageResult};
pub use stage_adapters::{
    CheckpointCopyExecutor, DataStreamFetchExecutor, CHECKPOINT_COPY_OPERATION,
    DATA_STREAM_FETCH_OPERATION,
};
pub use tool_runner::{ToolOutput, ToolRequest, ToolRunner};
