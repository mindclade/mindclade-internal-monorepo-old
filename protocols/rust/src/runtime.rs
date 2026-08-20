// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Runtime authority and worker-control protobuf projections.

// Prost projections mirror the canonical schema; boxing a generated variant
// would create a hand-maintained wire projection that diverges from generation.
#[allow(clippy::large_enum_variant)]
pub mod v1;
