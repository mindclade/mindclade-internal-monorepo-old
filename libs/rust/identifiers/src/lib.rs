// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Validated names, resource kinds, digests, and canonical resource identifiers.
#![forbid(unsafe_code)]
pub mod digest;
pub mod id;
pub mod kind;
mod name;
mod resource_id;
pub mod resource_version;
pub use kind::ResourceKind;
pub use name::{Name, NameError};
pub use resource_id::{
    ID_BODY_LENGTH, ID_SEPARATOR, MAXIMUM_KIND_LENGTH, MINIMUM_KIND_LENGTH, ResourceId,
    ResourceIdError,
};
