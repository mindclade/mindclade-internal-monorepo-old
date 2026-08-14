use mindclade_runtime_core::{
    ResourceKind, ResourceLimits, ResourceVector
};

pub fn gpu_budget(memory_bytes: u64, pinned_bytes: u64) -> ResourceVector {
    ResourceLimits::new().limit(ResourceKind::GpuMemoryEstimateBytes, memory_bytes).limit(ResourceKind::PinnedMemoryBytes, pinned_bytes).into_vector()
}
