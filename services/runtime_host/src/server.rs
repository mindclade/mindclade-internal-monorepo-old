// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Framework-independent runtime-host execution core.

use crate::ipc::validate_bulk_descriptors;
use crate::supervision::WorkerSession;
use crate::{
    HostConfig, HostHealth, ModelRegistry, NodeResources, ProcessLauncher, ProcessSupervisor,
};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_gpu_host::{DeviceCapability, GpuHost};
use mindclade_serving_runtime::{BatchEnvelope, host::HostInvocation};
use mindclade_worker_protocol::{
    BufferDescriptor, ExecutionTicket, RevocationSnapshot, SignatureVerifier, WorkerState,
};
use std::sync::Arc;

pub struct ExecutionSession {
    worker: WorkerSession,
    pub inputs: Vec<BufferDescriptor>,
    pub batches: Vec<BatchEnvelope>,
}

impl ExecutionSession {
    #[must_use]
    pub fn state(&self) -> WorkerState {
        self.worker.state()
    }
    #[must_use]
    pub fn ticket_id(&self) -> &str {
        self.worker.ticket_id()
    }
    #[must_use]
    pub fn fencing_token(&self) -> mindclade_runtime_core::FencingToken {
        self.worker.fencing_token()
    }
    pub fn drain(&self, reason: impl Into<String>) -> FaultResult<()> {
        self.worker.drain(reason)
    }
    pub fn cancel(&self, reason: impl Into<String>) -> FaultResult<()> {
        self.worker.cancel(reason)
    }
    pub fn commit(&self) -> FaultResult<()> {
        self.worker.commit()
    }
}

pub struct HostCore {
    config: HostConfig,
    resources: NodeResources,
    processes: Arc<ProcessSupervisor>,
    models: Arc<ModelRegistry>,
    health: Arc<HostHealth>,
}

impl HostCore {
    pub fn new(
        config: HostConfig,
        capability: DeviceCapability,
        launcher: Arc<dyn ProcessLauncher>,
        health: Arc<HostHealth>,
    ) -> FaultResult<Self> {
        config.validate()?;
        let resources = NodeResources::new(config.node_resources.clone())?;
        let gpu = Arc::new(GpuHost::new(capability, resources.root())?);
        let processes = Arc::new(ProcessSupervisor::new(launcher, config.maximum_processes)?);
        let models = Arc::new(ModelRegistry::new(
            gpu,
            processes.clone(),
            config.maximum_model_slots,
        )?);
        health.set_process_supervisor_ready(true);
        health.set_gpu_ready(true);
        Ok(Self {
            config,
            resources,
            processes,
            models,
            health,
        })
    }
    #[must_use]
    pub fn models(&self) -> Arc<ModelRegistry> {
        self.models.clone()
    }
    #[must_use]
    pub fn processes(&self) -> Arc<ProcessSupervisor> {
        self.processes.clone()
    }
    pub fn begin_execution<V: SignatureVerifier + ?Sized>(
        &self,
        ticket: &ExecutionTicket,
        inputs: Vec<BufferDescriptor>,
        now_unix_millis: u64,
        minimum_policy_epoch: u64,
        minimum_route_version: u64,
        minimum_revocation_epoch: u64,
        revocations: &RevocationSnapshot,
        verifier: &V,
    ) -> FaultResult<ExecutionSession> {
        if !self.health.snapshot().accepting {
            return Err(Fault::new(
                Code::Unavailable,
                "runtime host is draining or not accepting",
            ));
        }
        validate_bulk_descriptors(&inputs, self.config.maximum_input_buffers, now_unix_millis)?;
        revocations.validate(now_unix_millis, minimum_revocation_epoch, verifier)?;
        if let Some(model) = ticket.claims.model_bundle {
            if !self.models.contains(&model) {
                return Err(Fault::new(
                    Code::FailedPrecondition,
                    "required model bundle is not loaded on this runtime host",
                ));
            }
        }
        let budget = self.resources.child(
            ticket.claims.ticket_id.to_string(),
            ticket.claims.budget.to_resources(),
        );
        let worker = WorkerSession::start(
            budget,
            ticket,
            now_unix_millis,
            minimum_policy_epoch,
            minimum_route_version,
            revocations,
            verifier,
        )?;
        Ok(ExecutionSession {
            worker,
            inputs,
            batches: Vec::new(),
        })
    }
    pub fn begin_invocation<V: SignatureVerifier + ?Sized>(
        &self,
        invocation: HostInvocation,
        now_unix_millis: u64,
        minimum_policy_epoch: u64,
        minimum_route_version: u64,
        minimum_revocation_epoch: u64,
        revocations: &RevocationSnapshot,
        verifier: &V,
    ) -> FaultResult<ExecutionSession> {
        for batch in &invocation.batches {
            batch.validate()?;
        }
        let mut session = self.begin_execution(
            &invocation.ticket,
            invocation.inputs,
            now_unix_millis,
            minimum_policy_epoch,
            minimum_route_version,
            minimum_revocation_epoch,
            revocations,
            verifier,
        )?;
        session.batches = invocation.batches;
        Ok(session)
    }
    pub fn begin_drain(&self) {
        self.health.set_accepting(false);
    }
    pub fn resume_admission(&self) {
        self.health.set_accepting(true);
    }
    pub fn shutdown(&self) -> FaultResult<()> {
        self.begin_drain();
        self.processes.stop_all()
    }
}
