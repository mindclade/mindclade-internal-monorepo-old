// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Tenant/workspace access context propagated from validated authority.
use mindclade_faults::{Fault, FaultResult};
use mindclade_identifiers::ResourceId;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AccessContext {
    pub tenant_id: ResourceId,
    pub workspace_id: ResourceId,
    pub principal_id: String,
}
impl AccessContext {
    pub fn validate(&self) -> FaultResult<()> {
        if self.tenant_id.kind() != "tenant"
            || self.workspace_id.kind() != "workspace"
            || self.principal_id.trim().is_empty()
            || self.principal_id.len() > 512
        {
            return Err(Fault::invalid_argument(
                "artifact access context is invalid",
            ));
        }
        Ok(())
    }
}
