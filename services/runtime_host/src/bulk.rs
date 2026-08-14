//! Shared bulk-buffer broker re-exported for runtime-host consumers.
//!
//! The implementation lives in `mindclade_ipc_os` so runtime-host and
//! node-agent use one bounded OS-IPC mechanism.

pub use mindclade_ipc_os::{BulkBackend, BulkBufferBroker, BulkSegment};
pub use mindclade_ipc_os::file::FileSegment;
pub use mindclade_worker_protocol::BufferDescriptor;
