// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use crate::DeviceCapability;
use mindclade_faults::{Fault, FaultResult};

pub fn from_inventory_line(line: &str) -> FaultResult<DeviceCapability> {
    let fields: Vec<_> = line.split(',').map(str::trim).collect();
    if fields.len() != 2 {
        return Err(Fault::invalid_argument(
            "AMD inventory line must contain architecture,memory_bytes",
        ));
    }
    let memory = fields[1].parse::<u64>().map_err(|error| {
        Fault::invalid_argument("AMD inventory memory is invalid").with_source(error)
    })?;
    let capability = DeviceCapability {
        vendor: "amd".into(),
        architecture: fields[0].into(),
        total_memory_bytes: memory,
    };
    capability.validate()?;
    Ok(capability)
}
