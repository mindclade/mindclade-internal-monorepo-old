// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Protobuf-compatible runtime v1 messages.

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ArtifactGrant {
    #[prost(string, repeated, tag = "1")]
    pub readable_digests: ::prost::alloc::vec::Vec<::prost::alloc::string::String>,
    #[prost(string, repeated, tag = "2")]
    pub writable_namespaces: ::prost::alloc::vec::Vec<::prost::alloc::string::String>,
    #[prost(uint64, tag = "3")]
    pub maximum_read_bytes: u64,
    #[prost(uint64, tag = "4")]
    pub maximum_write_bytes: u64,
    #[prost(bool, tag = "5")]
    pub allow_range_reads: bool,
    #[prost(bool, tag = "6")]
    pub allow_multipart_writes: bool,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct DetachedSignature {
    #[prost(string, tag = "1")]
    pub algorithm: ::prost::alloc::string::String,
    #[prost(string, tag = "2")]
    pub key_id: ::prost::alloc::string::String,
    #[prost(bytes = "vec", tag = "3")]
    pub value: ::prost::alloc::vec::Vec<u8>,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ExecutionBudget {
    #[prost(uint32, tag = "1")]
    pub cpu_millis: u32,
    #[prost(uint64, tag = "2")]
    pub resident_memory_bytes: u64,
    #[prost(uint64, tag = "3")]
    pub pinned_memory_bytes: u64,
    #[prost(uint64, tag = "4")]
    pub shared_memory_bytes: u64,
    #[prost(uint64, tag = "5")]
    pub local_disk_bytes: u64,
    #[prost(uint32, tag = "6")]
    pub open_file_descriptors: u32,
    #[prost(uint32, tag = "7")]
    pub object_store_requests: u32,
    #[prost(uint32, tag = "8")]
    pub queued_operations: u32,
    #[prost(uint32, tag = "9")]
    pub child_processes: u32,
    #[prost(uint32, tag = "10")]
    pub cpu_worker_threads: u32,
    #[prost(uint64, tag = "11")]
    pub gpu_memory_estimate_bytes: u64,
    #[prost(uint64, tag = "12")]
    pub checkpoint_staging_bytes: u64,
    #[prost(uint64, tag = "13")]
    pub telemetry_spool_bytes: u64,
    #[prost(uint64, tag = "14")]
    pub maximum_output_bytes: u64,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ExecutionTicketClaims {
    #[prost(string, tag = "1")]
    pub ticket_id: String,
    #[prost(string, tag = "2")]
    pub issuer: String,
    #[prost(string, tag = "3")]
    pub tenant_id: String,
    #[prost(string, tag = "4")]
    pub workspace_id: String,
    #[prost(string, tag = "5")]
    pub run_id: String,
    #[prost(string, tag = "6")]
    pub job_id: String,
    #[prost(string, tag = "7")]
    pub stage_id: String,
    #[prost(string, tag = "8")]
    pub request_id: String,
    #[prost(uint32, tag = "9")]
    pub attempt: u32,
    #[prost(uint64, tag = "10")]
    pub fencing_token: u64,
    #[prost(string, tag = "11")]
    pub model_bundle_digest: String,
    #[prost(string, tag = "12")]
    pub engine_bundle_digest: String,
    #[prost(string, tag = "13")]
    pub resolved_config_digest: String,
    #[prost(string, tag = "14")]
    pub reference_snapshot_digest: String,
    #[prost(message, optional, tag = "15")]
    pub artifacts: Option<ArtifactGrant>,
    #[prost(message, optional, tag = "16")]
    pub budget: Option<ExecutionBudget>,
    #[prost(string, tag = "17")]
    pub execution_class: String,
    #[prost(string, tag = "18")]
    pub accelerator_capability: String,
    #[prost(uint64, tag = "19")]
    pub not_before_unix_millis: u64,
    #[prost(uint64, tag = "20")]
    pub deadline_unix_millis: u64,
    #[prost(uint64, tag = "21")]
    pub expires_unix_millis: u64,
    #[prost(uint64, tag = "22")]
    pub policy_epoch: u64,
    #[prost(uint64, tag = "23")]
    pub route_snapshot_version: u64,
    #[prost(uint64, tag = "24")]
    pub revocation_epoch: u64,
    #[prost(string, tag = "25")]
    pub idempotency_key: String,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct ExecutionTicket {
    #[prost(message, optional, tag = "1")]
    pub claims: Option<ExecutionTicketClaims>,
    #[prost(message, optional, tag = "2")]
    pub signature: Option<DetachedSignature>,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct AdmissionGrantClaims {
    #[prost(string, tag = "1")]
    pub grant_id: String,
    #[prost(string, tag = "2")]
    pub tenant_id: String,
    #[prost(string, tag = "3")]
    pub principal_id: String,
    #[prost(string, repeated, tag = "4")]
    pub allowed_deployments: Vec<String>,
    #[prost(string, repeated, tag = "5")]
    pub allowed_capabilities: Vec<String>,
    #[prost(string, tag = "6")]
    pub region: String,
    #[prost(uint32, tag = "7")]
    pub maximum_concurrency: u32,
    #[prost(uint64, tag = "8")]
    pub maximum_requests: u64,
    #[prost(uint64, tag = "9")]
    pub maximum_input_units: u64,
    #[prost(uint64, tag = "10")]
    pub maximum_output_units: u64,
    #[prost(uint64, tag = "11")]
    pub not_before_unix_millis: u64,
    #[prost(uint64, tag = "12")]
    pub expires_unix_millis: u64,
    #[prost(uint64, tag = "13")]
    pub policy_epoch: u64,
    #[prost(uint64, tag = "14")]
    pub revocation_epoch: u64,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct AdmissionGrant {
    #[prost(message, optional, tag = "1")]
    pub claims: Option<AdmissionGrantClaims>,
    #[prost(message, optional, tag = "2")]
    pub signature: Option<DetachedSignature>,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct DeploymentRoute {
    #[prost(string, tag = "1")]
    pub deployment_id: String,
    #[prost(string, tag = "2")]
    pub model_bundle_digest: String,
    #[prost(string, tag = "3")]
    pub engine_bundle_digest: String,
    #[prost(string, tag = "4")]
    pub endpoint: String,
    #[prost(string, tag = "5")]
    pub region: String,
    #[prost(uint32, tag = "6")]
    pub weight: u32,
    #[prost(string, repeated, tag = "7")]
    pub capabilities: Vec<String>,
    #[prost(uint64, tag = "8")]
    pub lease_expires_unix_millis: u64,
    #[prost(string, tag = "9")]
    pub safety_policy_digest: String,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct RouteSnapshotClaims {
    #[prost(string, tag = "1")]
    pub snapshot_id: String,
    #[prost(string, tag = "2")]
    pub snapshot_digest: String,
    #[prost(uint64, tag = "3")]
    pub version: u64,
    #[prost(uint64, tag = "4")]
    pub policy_epoch: u64,
    #[prost(uint64, tag = "5")]
    pub revocation_epoch: u64,
    #[prost(uint64, tag = "6")]
    pub created_unix_millis: u64,
    #[prost(uint64, tag = "7")]
    pub expires_unix_millis: u64,
    #[prost(message, repeated, tag = "8")]
    pub routes: Vec<DeploymentRoute>,
    #[prost(string, tag = "9")]
    pub minimum_runtime_version: String,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct RouteSnapshot {
    #[prost(message, optional, tag = "1")]
    pub claims: Option<RouteSnapshotClaims>,
    #[prost(message, optional, tag = "2")]
    pub signature: Option<DetachedSignature>,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct RevocationSnapshotClaims {
    #[prost(uint64, tag = "1")]
    pub epoch: u64,
    #[prost(uint64, tag = "2")]
    pub created_unix_millis: u64,
    #[prost(uint64, tag = "3")]
    pub expires_unix_millis: u64,
    #[prost(string, repeated, tag = "4")]
    pub revoked_grant_ids: Vec<String>,
    #[prost(string, repeated, tag = "5")]
    pub revoked_ticket_ids: Vec<String>,
    #[prost(string, repeated, tag = "6")]
    pub revoked_deployment_ids: Vec<String>,
    #[prost(string, repeated, tag = "7")]
    pub revoked_bundle_digests: Vec<String>,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct RevocationSnapshot {
    #[prost(message, optional, tag = "1")]
    pub claims: Option<RevocationSnapshotClaims>,
    #[prost(message, optional, tag = "2")]
    pub signature: Option<DetachedSignature>,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct BufferDescriptor {
    #[prost(string, tag = "1")]
    pub segment_id: String,
    #[prost(uint64, tag = "2")]
    pub generation: u64,
    #[prost(uint64, tag = "3")]
    pub offset_bytes: u64,
    #[prost(uint64, tag = "4")]
    pub length_bytes: u64,
    #[prost(string, tag = "5")]
    pub element_type: String,
    #[prost(uint64, repeated, tag = "6")]
    pub shape: Vec<u64>,
    #[prost(string, tag = "7")]
    pub content_digest: String,
    #[prost(string, tag = "8")]
    pub owner_process: String,
    #[prost(uint64, tag = "9")]
    pub lease_expires_unix_millis: u64,
    #[prost(enumeration = "AccessMode", tag = "10")]
    pub access_mode: i32,
    #[prost(enumeration = "Transport", tag = "11")]
    pub transport: i32,
    #[prost(string, tag = "12")]
    pub locator: String,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, PartialOrd, Ord, ::prost::Enumeration)]
#[repr(i32)]
pub enum AccessMode {
    Unspecified = 0,
    ReadOnly = 1,
    ReadWrite = 2,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, PartialOrd, Ord, ::prost::Enumeration)]
#[repr(i32)]
pub enum Transport {
    Unspecified = 0,
    SharedMemory = 1,
    FileDescriptor = 2,
    LocalFile = 3,
    ArtifactRef = 4,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct StartCommand {
    #[prost(message, optional, tag = "1")]
    pub ticket: Option<ExecutionTicket>,
    #[prost(message, repeated, tag = "2")]
    pub inputs: Vec<BufferDescriptor>,
    #[prost(string, tag = "3")]
    pub operation: String,
}
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct CancelCommand {
    #[prost(string, tag = "1")]
    pub reason: String,
    #[prost(uint64, tag = "2")]
    pub deadline_unix_millis: u64,
}
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct DrainCommand {
    #[prost(string, tag = "1")]
    pub reason: String,
    #[prost(uint64, tag = "2")]
    pub deadline_unix_millis: u64,
}
#[derive(Clone, PartialEq, ::prost::Message)]
pub struct HeartbeatCommand {
    #[prost(uint64, tag = "1")]
    pub requested_at_unix_millis: u64,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct WorkerCommand {
    #[prost(uint64, tag = "1")]
    pub sequence: u64,
    #[prost(oneof = "worker_command::Command", tags = "2, 3, 4, 5")]
    pub command: Option<worker_command::Command>,
}
pub mod worker_command {
    #[derive(Clone, PartialEq, ::prost::Oneof)]
    pub enum Command {
        #[prost(message, tag = "2")]
        Start(super::StartCommand),
        #[prost(message, tag = "3")]
        Cancel(super::CancelCommand),
        #[prost(message, tag = "4")]
        Drain(super::DrainCommand),
        #[prost(message, tag = "5")]
        Heartbeat(super::HeartbeatCommand),
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, PartialOrd, Ord, ::prost::Enumeration)]
#[repr(i32)]
pub enum WorkerState {
    Unspecified = 0,
    Created = 1,
    Starting = 2,
    Ready = 3,
    Leased = 4,
    Running = 5,
    Draining = 6,
    Committing = 7,
    Completed = 8,
    Recovering = 9,
    Cancelling = 10,
    Cancelled = 11,
    Failed = 12,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct WorkerStatus {
    #[prost(uint64, tag = "1")]
    pub sequence: u64,
    #[prost(string, tag = "2")]
    pub ticket_id: String,
    #[prost(uint64, tag = "3")]
    pub fencing_token: u64,
    #[prost(enumeration = "WorkerState", tag = "4")]
    pub state: i32,
    #[prost(uint64, tag = "5")]
    pub observed_unix_millis: u64,
    #[prost(string, tag = "6")]
    pub message: String,
    #[prost(message, repeated, tag = "7")]
    pub outputs: Vec<BufferDescriptor>,
    #[prost(string, tag = "8")]
    pub diagnostic_artifact_digest: String,
}

/// Maps validation faults at the transport edge without leaking internals.
#[must_use]
pub fn invalid_argument_status(message: &'static str) -> tonic::Status {
    tonic::Status::invalid_argument(message)
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct RuntimeDispatchRequest {
    #[prost(string, tag = "1")]
    pub request_id: String,
    #[prost(message, optional, tag = "2")]
    pub grant: Option<AdmissionGrant>,
    #[prost(bytes = "vec", tag = "3")]
    pub request_key: Vec<u8>,
    #[prost(string, tag = "4")]
    pub deployment_hint: String,
    #[prost(string, repeated, tag = "5")]
    pub required_capabilities: Vec<String>,
    #[prost(uint64, tag = "6")]
    pub input_units: u64,
    #[prost(uint64, tag = "7")]
    pub output_units: u64,
    #[prost(string, tag = "8")]
    pub payload_descriptor: String,
}

#[derive(Clone, PartialEq, ::prost::Message)]
pub struct RuntimeDispatchResponse {
    #[prost(string, tag = "1")]
    pub request_id: String,
    #[prost(string, tag = "2")]
    pub deployment_id: String,
    #[prost(string, tag = "3")]
    pub endpoint: String,
}

// The execution service is generated because Tonic's transport glue is not a
// stable hand-written API. Imported runtime messages remain the committed
// projections above, so Cargo and Bazel consumers retain their existing types.
// Generated transport glue follows Tonic's lint policy rather than this
// workspace's pedantic hand-written-code policy.
#[allow(clippy::all, clippy::pedantic)]
mod generated_execution_service {
    include!(concat!(env!("OUT_DIR"), "/mindclade.runtime.v1.rs"));
}

pub use generated_execution_service::*;
