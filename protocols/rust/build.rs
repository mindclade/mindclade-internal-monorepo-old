// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use std::path::PathBuf;

fn well_known_proto_root() -> Result<PathBuf, Box<dyn std::error::Error>> {
    if let Ok(protos) = std::env::var("PROTOBUF_WELL_KNOWN_PROTOS") {
        let timestamp = protos
            .split_ascii_whitespace()
            .map(PathBuf::from)
            .find(|path| path.ends_with("google/protobuf/timestamp.proto"))
            .ok_or_else(|| {
                std::io::Error::new(
                    std::io::ErrorKind::InvalidInput,
                    "PROTOBUF_WELL_KNOWN_PROTOS does not contain timestamp.proto",
                )
            })?;
        return timestamp
            .ancestors()
            .nth(3)
            .map(std::path::Path::to_path_buf)
            .ok_or_else(|| {
                std::io::Error::new(
                    std::io::ErrorKind::InvalidInput,
                    "timestamp.proto has no protobuf include root",
                )
                .into()
            });
    }
    Ok(protoc_bin_vendored::include_path()?)
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let manifest = PathBuf::from(std::env::var("CARGO_MANIFEST_DIR")?);
    let proto_root = manifest.join("../proto");
    let execution = proto_root.join("mindclade/runtime/v1/execution_service.proto");
    let checkpoint_contracts = [
        proto_root.join("mindclade/common/v1/artifact_ref.proto"),
        proto_root.join("mindclade/training/v1/topology.proto"),
        proto_root.join("mindclade/training/v1/progress.proto"),
        proto_root.join("mindclade/training/v1/checkpoint.proto"),
        proto_root.join("mindclade/training/v1/run.proto"),
        proto_root.join("mindclade/artifact/v1/checkpoint.proto"),
        proto_root.join("mindclade/registry/v1/checkpoint.proto"),
    ];
    for source in std::iter::once(&execution).chain(checkpoint_contracts.iter()) {
        println!("cargo:rerun-if-changed={}", source.display());
    }
    let mut builder = tonic_prost_build::configure()
        .build_client(true)
        .build_server(true);
    for name in [
        "ArtifactGrant",
        "DetachedSignature",
        "DeploymentRoute",
        "RouteSnapshotClaims",
        "RouteSnapshot",
        "RevocationSnapshotClaims",
        "RevocationSnapshot",
        "ExecutionBudget",
        "ExecutionTicketClaims",
        "ExecutionTicket",
        "AdmissionGrantClaims",
        "AdmissionGrant",
        "BufferDescriptor",
        "WorkerCommand",
        "StartCommand",
        "CancelCommand",
        "DrainCommand",
        "HeartbeatCommand",
        "WorkerStatus",
        "WorkerState",
        "RuntimeDispatchRequest",
        "RuntimeDispatchResponse",
    ] {
        builder = builder.extern_path(
            format!(".mindclade.runtime.v1.{name}"),
            format!("crate::runtime::v1::{name}"),
        );
    }
    let well_known_include = well_known_proto_root()?;
    let mut prost_config = tonic_prost_build::Config::new();
    if std::env::var_os("PROTOC").is_none() {
        // Bazel declares its pinned protoc through PROTOC. Plain Cargo builds have no tool
        // dependency mechanism, so use a lockfile-pinned binary instead of whichever compiler
        // happens to be installed on the workstation or hosted runner.
        prost_config.protoc_executable(protoc_bin_vendored::protoc_bin_path()?);
    }
    let mut sources = Vec::with_capacity(checkpoint_contracts.len() + 1);
    sources.push(execution);
    sources.extend(checkpoint_contracts);
    builder.compile_with_config(prost_config, &sources, &[proto_root, well_known_include])?;
    Ok(())
}
