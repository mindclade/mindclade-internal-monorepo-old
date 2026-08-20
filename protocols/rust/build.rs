// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary

use std::path::PathBuf;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let manifest = PathBuf::from(std::env::var("CARGO_MANIFEST_DIR")?);
    let proto_root = manifest.join("../proto");
    let execution = proto_root.join("mindclade/runtime/v1/execution_service.proto");
    println!("cargo:rerun-if-changed={}", execution.display());
    println!("cargo:rerun-if-changed={}", proto_root.display());
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
    builder.compile_protos(&[execution], &[proto_root])?;
    Ok(())
}
