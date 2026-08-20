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
use std::sync::{Arc, Mutex};

#[derive(Debug)]
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
    pub fn fail(&self, message: impl Into<String>) -> FaultResult<()> {
        self.worker.fail(message)
    }
}

#[derive(Debug)]
pub struct HostCore {
    config: HostConfig,
    resources: NodeResources,
    processes: Arc<ProcessSupervisor>,
    models: Arc<ModelRegistry>,
    health: Arc<HostHealth>,
    admission: Mutex<bool>,
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
            admission: Mutex::new(false),
        })
    }
    #[must_use]
    pub fn models(&self) -> Arc<ModelRegistry> {
        self.models.clone()
    }
    #[must_use]
    pub(crate) const fn config(&self) -> &HostConfig {
        &self.config
    }
    #[must_use]
    pub fn processes(&self) -> Arc<ProcessSupervisor> {
        self.processes.clone()
    }
    // These explicit policy floors are security inputs and intentionally stay
    // visible at the trust-boundary call instead of hiding in ambient state.
    #[allow(clippy::too_many_arguments)]
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
        // Keep the gate through ticket validation and resource reservation so
        // `begin_drain` cannot return while a new session is still starting.
        let accepting = self
            .admission
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner);
        if !*accepting || !self.health.snapshot().accepting {
            return Err(Fault::new(
                Code::Unavailable,
                "runtime host is draining or not accepting",
            ));
        }
        validate_bulk_descriptors(&inputs, self.config.maximum_input_buffers, now_unix_millis)?;
        revocations.validate(now_unix_millis, minimum_revocation_epoch, verifier)?;
        if let Some(model) = ticket.claims.model_bundle
            && !self.models.contains_running(&model)?
        {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "required model bundle is not loaded on this runtime host",
            ));
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
    #[allow(clippy::too_many_arguments)]
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
        *self
            .admission
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner) = false;
        self.health.set_accepting(false);
    }
    pub fn resume_admission(&self) {
        self.health.set_accepting(true);
        *self
            .admission
            .lock()
            .unwrap_or_else(std::sync::PoisonError::into_inner) = true;
    }
    pub fn shutdown(&self) -> FaultResult<()> {
        self.begin_drain();
        self.processes.stop_all()
    }
}
