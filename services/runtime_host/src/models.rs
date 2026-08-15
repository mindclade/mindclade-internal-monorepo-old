// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Model-slot and process registry. It does not interpret tensors or model code.

use crate::{ProcessHandle, ProcessSpec, ProcessSupervisor};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_gpu_host::{Digest, GpuHost, ModelSlot, ModelSlotRequest};
use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ModelSpec {
    pub model_digest: Digest,
    pub minimum_gpu_memory_bytes: u64,
    pub pinned_memory_bytes: u64,
    pub process: ProcessSpec,
}
impl ModelSpec {
    pub fn validate(&self) -> FaultResult<()> {
        if self.model_digest == Digest::ZERO || self.minimum_gpu_memory_bytes == 0 {
            return Err(Fault::invalid_argument(
                "model slot specification is invalid",
            ));
        }
        self.process.validate()
    }
}

pub struct LoadedModel {
    pub spec: ModelSpec,
    pub process: ProcessHandle,
    _slot: ModelSlot,
}
impl core::fmt::Debug for LoadedModel {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("LoadedModel")
            .field("model_digest", &self.spec.model_digest)
            .field("pid", &self.process.pid)
            .finish()
    }
}

pub struct ModelRegistry {
    gpu: Arc<GpuHost>,
    processes: Arc<ProcessSupervisor>,
    maximum_slots: u32,
    models: Mutex<BTreeMap<Digest, LoadedModel>>,
}
impl core::fmt::Debug for ModelRegistry {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("ModelRegistry")
            .field("loaded", &self.len())
            .field("maximum_slots", &self.maximum_slots)
            .finish()
    }
}
impl ModelRegistry {
    pub fn new(
        gpu: Arc<GpuHost>,
        processes: Arc<ProcessSupervisor>,
        maximum_slots: u32,
    ) -> FaultResult<Self> {
        if maximum_slots == 0 {
            return Err(Fault::invalid_argument(
                "maximum model slots must be positive",
            ));
        }
        Ok(Self {
            gpu,
            processes,
            maximum_slots,
            models: Mutex::new(BTreeMap::new()),
        })
    }
    pub fn load(&self, spec: ModelSpec) -> FaultResult<()> {
        spec.validate()?;
        let mut models = self
            .models
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        if models.contains_key(&spec.model_digest) {
            return Ok(());
        }
        let maximum_slots = usize::try_from(self.maximum_slots)
            .map_err(|_| Fault::new(Code::OutOfRange, "model slot limit exceeds platform usize"))?;
        if models.len() >= maximum_slots {
            return Err(Fault::new(
                Code::ResourceExhausted,
                "model slot limit reached",
            ));
        }
        let slot = self.gpu.reserve_model(ModelSlotRequest {
            model_digest: spec.model_digest,
            minimum_memory_bytes: spec.minimum_gpu_memory_bytes,
            pinned_memory_bytes: spec.pinned_memory_bytes,
        })?;
        let process = self.processes.launch(&spec.process)?;
        models.insert(
            spec.model_digest,
            LoadedModel {
                spec,
                process,
                _slot: slot,
            },
        );
        Ok(())
    }
    pub fn unload(&self, digest: &Digest) -> FaultResult<()> {
        let process_name = self
            .models
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .get(digest)
            .map(|loaded| loaded.spec.process.name.clone());
        let Some(process_name) = process_name else {
            return Ok(());
        };

        // Do not release the GPU/model reservation until the supervised worker
        // is confirmed stopped. A termination failure must leave ownership and
        // accounting intact rather than creating an unbudgeted live process.
        self.processes.stop(&process_name)?;
        self.models
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .remove(digest);
        Ok(())
    }
    #[must_use]
    pub fn contains(&self, digest: &Digest) -> bool {
        self.models
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .contains_key(digest)
    }
    #[must_use]
    pub fn len(&self) -> usize {
        self.models
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .len()
    }
}
