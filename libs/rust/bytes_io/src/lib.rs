// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Checked byte arithmetic, bounded copying, and reusable process-local buffers.
#![forbid(unsafe_code)]
pub mod alignment;
pub mod buffer;
mod copy;
pub mod metrics;
mod pool;
pub mod range;
mod spec;
pub mod vectored;
pub use copy::{CopyReport, copy_bounded};
pub use pool::BufferPool;
pub use spec::{Alignment, ByteBudget, ByteRange, ByteSize, Reservation};
