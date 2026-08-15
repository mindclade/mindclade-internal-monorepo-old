// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

use mindclade_faults::FaultResult;
use mindclade_worker_protocol::BufferDescriptor;

pub fn validate_descriptor(descriptor: &BufferDescriptor, now_unix_millis: u64) -> FaultResult<()> {
    descriptor.validate(now_unix_millis)
}
