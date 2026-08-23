// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Runtime-host configuration: hard limits, and the declared settings surface.
//!
//! The settings catalog lives here rather than in `bootstrap.rs` so the
//! *declaration* of what the host reads is separate from the *assembly* of what
//! it builds. `bootstrap.rs` previously carried a private copy of the
//! `required` / `parse_u64` / `parse_positive_u64` / `absolute_path` helper set
//! — the same set `services/runtime_gateway` and `services/ai_gateway_proxy`
//! had each grown independently, differing only in the error-message string.
//! All three now resolve through `mindclade_config`.

use mindclade_config::{Catalog, EnvSource, Field};
use mindclade_faults::{Fault, FaultResult};
use mindclade_ipc::MAX_CONTROL_PAYLOAD;
use mindclade_runtime_core::{ResourceKind, ResourceVector};

/// Namespace label carried by every configuration fault this service raises.
pub const NAMESPACE: &str = "runtime-host";

/// Largest environment value the host accepts, in bytes.
///
/// Preserves the pre-migration bound: the replaced `required` helper rejected a
/// value over 4 KiB before it reached any parser.
const MAX_VALUE_BYTES: usize = 4096;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct HostConfig {
    pub maximum_processes: u32,
    pub maximum_model_slots: u32,
    pub maximum_input_buffers: usize,
    pub maximum_control_payload_bytes: u64,
    pub node_resources: ResourceVector,
}

impl HostConfig {
    pub fn validate(&self) -> FaultResult<()> {
        if self.maximum_processes == 0
            || self.maximum_model_slots == 0
            || self.maximum_model_slots > self.maximum_processes
            || self.maximum_input_buffers == 0
            || self.maximum_input_buffers > 4_096
            || self.maximum_control_payload_bytes == 0
            || self.maximum_control_payload_bytes > MAX_CONTROL_PAYLOAD.get()
            || self.node_resources.get(ResourceKind::ResidentMemoryBytes) == 0
            || self.node_resources.get(ResourceKind::OpenFileDescriptors) == 0
            || self.node_resources.get(ResourceKind::Processes) == 0
            || self.node_resources.get(ResourceKind::CpuThreads) == 0
        {
            return Err(Fault::invalid_argument(
                "runtime host configuration is invalid",
            ));
        }
        Ok(())
    }
}

/// Canonical key to environment variable name, for every setting the host reads
/// unconditionally.
///
/// The right-hand column is the deployed contract. `tests/settings.rs` asserts
/// this table against a literal expectation, so a rename cannot pass review as
/// a variable the process quietly stops reading.
pub const BINDINGS: &[(&str, &str)] = &[
    ("gpu.arch", "MINDCLADE_RUNTIME_GPU_ARCH"),
    ("gpu.memory.bytes", "MINDCLADE_RUNTIME_GPU_MEMORY_BYTES"),
    ("gpu.vendor", "MINDCLADE_RUNTIME_GPU_VENDOR"),
    ("host.grpc.socket", "MINDCLADE_RUNTIME_HOST_GRPC_SOCKET"),
    ("host.socket", "MINDCLADE_RUNTIME_HOST_SOCKET"),
    ("key.id", "MINDCLADE_RUNTIME_KEY_ID"),
    ("key.not.after.ms", "MINDCLADE_RUNTIME_KEY_NOT_AFTER_MS"),
    ("key.not.before.ms", "MINDCLADE_RUNTIME_KEY_NOT_BEFORE_MS"),
    ("key.public.hex", "MINDCLADE_RUNTIME_PUBLIC_KEY_HEX"),
    ("max.control.bytes", "MINDCLADE_RUNTIME_MAX_CONTROL_BYTES"),
    ("max.input.buffers", "MINDCLADE_RUNTIME_MAX_INPUT_BUFFERS"),
    ("max.model.slots", "MINDCLADE_RUNTIME_MAX_MODEL_SLOTS"),
    ("max.processes", "MINDCLADE_RUNTIME_MAX_PROCESSES"),
    ("min.policy.epoch", "MINDCLADE_RUNTIME_MIN_POLICY_EPOCH"),
    (
        "min.revocation.epoch",
        "MINDCLADE_RUNTIME_MIN_REVOCATION_EPOCH",
    ),
    ("min.route.version", "MINDCLADE_RUNTIME_MIN_ROUTE_VERSION"),
    (
        "model.bundle.digest",
        "MINDCLADE_RUNTIME_MODEL_BUNDLE_DIGEST",
    ),
    (
        "model.worker.config",
        "MINDCLADE_RUNTIME_MODEL_WORKER_CONFIG",
    ),
    (
        "model.worker.executable",
        "MINDCLADE_RUNTIME_MODEL_WORKER_EXECUTABLE",
    ),
    (
        "model.worker.socket",
        "MINDCLADE_RUNTIME_MODEL_WORKER_SOCKET",
    ),
    (
        "node.checkpoint.staging.bytes",
        "MINDCLADE_RUNTIME_NODE_CHECKPOINT_STAGING_BYTES",
    ),
    ("node.cpu.millis", "MINDCLADE_RUNTIME_NODE_CPU_MILLIS"),
    ("node.cpu.threads", "MINDCLADE_RUNTIME_NODE_CPU_THREADS"),
    ("node.disk.bytes", "MINDCLADE_RUNTIME_NODE_DISK_BYTES"),
    (
        "node.gpu.memory.bytes",
        "MINDCLADE_RUNTIME_NODE_GPU_MEMORY_BYTES",
    ),
    ("node.memory.bytes", "MINDCLADE_RUNTIME_NODE_MEMORY_BYTES"),
    (
        "node.object.requests",
        "MINDCLADE_RUNTIME_NODE_OBJECT_REQUESTS",
    ),
    ("node.open.fds", "MINDCLADE_RUNTIME_NODE_OPEN_FDS"),
    (
        "node.pinned.memory.bytes",
        "MINDCLADE_RUNTIME_NODE_PINNED_MEMORY_BYTES",
    ),
    ("node.processes", "MINDCLADE_RUNTIME_NODE_PROCESSES"),
    (
        "node.queued.requests",
        "MINDCLADE_RUNTIME_NODE_QUEUED_REQUESTS",
    ),
    (
        "node.shared.memory.bytes",
        "MINDCLADE_RUNTIME_NODE_SHARED_MEMORY_BYTES",
    ),
    (
        "node.telemetry.spool.bytes",
        "MINDCLADE_RUNTIME_NODE_TELEMETRY_SPOOL_BYTES",
    ),
    (
        "revocation.snapshot.file",
        "MINDCLADE_RUNTIME_REVOCATION_SNAPSHOT_FILE",
    ),
];

/// Settings read only when a preloaded model worker is configured.
///
/// They are a second catalog rather than optional fields in the first because
/// their requiredness is conditional: a host with no preloaded model must start
/// without `MINDCLADE_RUNTIME_MODEL_GPU_MEMORY_BYTES` set, and a host *with*
/// one must refuse to start without it. Declaring them optional in the main
/// catalog would silently accept the second case.
pub const MODEL_BINDINGS: &[(&str, &str)] = &[
    (
        "model.gpu.memory.bytes",
        "MINDCLADE_RUNTIME_MODEL_GPU_MEMORY_BYTES",
    ),
    (
        "model.pinned.memory.bytes",
        "MINDCLADE_RUNTIME_MODEL_PINNED_MEMORY_BYTES",
    ),
];

/// A required setting under the host's raw-value policy.
///
/// Non-empty, no leading or trailing whitespace, at most 4 KiB — exactly what
/// the replaced `required` helper enforced, including that a whitespace-only
/// value is *invalid* rather than *missing*.
fn required(key: &'static str, doc: &'static str) -> Field {
    Field::required(key, doc)
        .reject_surrounding_whitespace()
        .maximum_bytes(MAX_VALUE_BYTES)
}

/// An optional resource ceiling that defaults to zero when unset or empty.
///
/// The replaced `parse_optional_u64` treated an empty value as absent, unlike
/// this service's required reads. That difference is preserved rather than
/// normalized: zero means "unconstrained" in `ResourceVector`, and turning an
/// empty value into a parse failure would refuse hosts that start today.
fn optional_amount(key: &'static str, doc: &'static str) -> Field {
    Field::defaulted(key, doc, "0")
        .empty_uses_default()
        .maximum_bytes(MAX_VALUE_BYTES)
}

/// A member of the preloaded-model group, present or absent as a unit.
fn model_group(key: &'static str, doc: &'static str) -> Field {
    Field::defaulted(key, doc, "")
}

/// The complete settings surface the host reads unconditionally.
///
/// Split into groups because the whole surface is 34 settings and a single
/// builder chain that long stops being reviewable — the point of the catalog is
/// that a reader can see what the host consumes.
pub fn catalog() -> FaultResult<Catalog> {
    let catalog = Catalog::new(NAMESPACE)?;
    let catalog = declare_transport(catalog)?;
    let catalog = declare_authority(catalog)?;
    let catalog = declare_limits(catalog)?;
    let catalog = declare_node_capacity(catalog)?;
    let catalog = declare_accelerator(catalog)?;
    declare_model_group(catalog)
}

fn declare_transport(catalog: Catalog) -> FaultResult<Catalog> {
    catalog
        .declare(required(
            "host.socket",
            "Absolute path of the control IPC socket the host listens on.",
        ))?
        .declare(required(
            "host.grpc.socket",
            "Absolute path of the gRPC worker-control socket; must differ from the control socket.",
        ))
}

fn declare_authority(catalog: Catalog) -> FaultResult<Catalog> {
    catalog
        .declare(required(
            "key.id",
            "Identifier of the Ed25519 key that signs execution tickets.",
        ))?
        .declare(required(
            "key.public.hex",
            "Ed25519 verification key as 64 hexadecimal characters.",
        ))?
        .declare(required(
            "key.not.before.ms",
            "Unix milliseconds at which the verification key becomes valid.",
        ))?
        .declare(required(
            "key.not.after.ms",
            "Unix milliseconds at which the verification key expires.",
        ))?
        .declare(required(
            "revocation.snapshot.file",
            "Absolute path of the signed revocation snapshot loaded at startup.",
        ))?
        .declare(required(
            "min.policy.epoch",
            "Lowest policy epoch the host will admit; raises the local authority floor.",
        ))?
        .declare(required(
            "min.route.version",
            "Lowest route version the host will admit.",
        ))?
        .declare(required(
            "min.revocation.epoch",
            "Lowest revocation epoch the host will admit.",
        ))
}

fn declare_limits(catalog: Catalog) -> FaultResult<Catalog> {
    catalog
        .declare(required(
            "max.processes",
            "Ceiling on concurrently supervised worker processes.",
        ))?
        .declare(required(
            "max.model.slots",
            "Ceiling on resident model slots; never above the process ceiling.",
        ))?
        .declare(required(
            "max.input.buffers",
            "Ceiling on pooled bulk input buffers.",
        ))?
        .declare(required(
            "max.control.bytes",
            "Ceiling on a single control payload, bounded by the IPC maximum.",
        ))
}

fn declare_node_capacity(catalog: Catalog) -> FaultResult<Catalog> {
    catalog
        .declare(required(
            "node.cpu.millis",
            "Node CPU capacity in millicores.",
        ))?
        .declare(required(
            "node.memory.bytes",
            "Node resident memory capacity in bytes.",
        ))?
        .declare(optional_amount(
            "node.pinned.memory.bytes",
            "Node pinned-memory capacity in bytes; zero leaves it unconstrained.",
        ))?
        .declare(optional_amount(
            "node.shared.memory.bytes",
            "Node shared-memory capacity in bytes; zero leaves it unconstrained.",
        ))?
        .declare(optional_amount(
            "node.disk.bytes",
            "Node local-disk capacity in bytes; zero leaves it unconstrained.",
        ))?
        .declare(required(
            "node.open.fds",
            "Node open file-descriptor capacity.",
        ))?
        .declare(optional_amount(
            "node.object.requests",
            "Node object-store request capacity; zero leaves it unconstrained.",
        ))?
        .declare(optional_amount(
            "node.queued.requests",
            "Node queued-request capacity; zero leaves it unconstrained.",
        ))?
        .declare(required("node.processes", "Node process capacity."))?
        .declare(required("node.cpu.threads", "Node CPU-thread capacity."))?
        .declare(required(
            "node.gpu.memory.bytes",
            "Node GPU memory capacity in bytes.",
        ))?
        .declare(optional_amount(
            "node.checkpoint.staging.bytes",
            "Node checkpoint staging capacity in bytes; zero leaves it unconstrained.",
        ))?
        .declare(optional_amount(
            "node.telemetry.spool.bytes",
            "Node telemetry spool capacity in bytes; zero leaves it unconstrained.",
        ))
}

fn declare_accelerator(catalog: Catalog) -> FaultResult<Catalog> {
    catalog
        .declare(required("gpu.vendor", "Accelerator vendor of this node."))?
        .declare(required(
            "gpu.arch",
            "Accelerator architecture of this node.",
        ))?
        .declare(required(
            "gpu.memory.bytes",
            "Total accelerator memory of this node in bytes.",
        ))
}

fn declare_model_group(catalog: Catalog) -> FaultResult<Catalog> {
    catalog
        .declare(model_group(
            "model.bundle.digest",
            "Digest of the preloaded model bundle. Set with the other three model \
             settings, or with none of them.",
        ))?
        .declare(model_group(
            "model.worker.executable",
            "Absolute path of the model-worker executable to preload.",
        ))?
        .declare(model_group(
            "model.worker.socket",
            "Absolute path of the model-worker control socket.",
        ))?
        .declare(model_group(
            "model.worker.config",
            "Absolute path of the model-worker configuration file.",
        ))
}

/// The settings read only once a preloaded model worker is configured.
pub fn model_catalog() -> FaultResult<Catalog> {
    Catalog::new(NAMESPACE)?
        .declare(required(
            "model.gpu.memory.bytes",
            "Minimum accelerator memory the preloaded model requires, in bytes.",
        ))?
        .declare(optional_amount(
            "model.pinned.memory.bytes",
            "Pinned memory the preloaded model reserves, in bytes.",
        ))
}

/// Binds every unconditional setting to its environment variable name.
#[must_use]
pub fn bind(source: EnvSource) -> EnvSource {
    BINDINGS
        .iter()
        .fold(source, |bound, (key, variable)| bound.bind(*key, *variable))
}

/// Binds the preloaded-model settings to their environment variable names.
#[must_use]
pub fn bind_model(source: EnvSource) -> EnvSource {
    MODEL_BINDINGS
        .iter()
        .fold(source, |bound, (key, variable)| bound.bind(*key, *variable))
}
