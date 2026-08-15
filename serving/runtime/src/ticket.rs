// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Canonical local ticket/grant validation facade over `worker_protocol`.

pub use mindclade_worker_protocol::{
    AdmissionGrant, ArtifactGrant, ExecutionBudget, ExecutionTicket, RevocationSnapshot,
    RouteSnapshot, SignatureVerifier,
};
