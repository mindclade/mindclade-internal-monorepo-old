// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Per-execution worker-runtime supervision.

use mindclade_faults::{Fault, FaultResult};
use mindclade_runtime_core::{Budget, FencingToken};
use mindclade_worker_protocol::{
    ExecutionTicket, RevocationSnapshot, SignatureVerifier, WorkerState,
};
use mindclade_worker_runtime::WorkerRuntime;
use std::sync::Arc;

#[derive(Debug)]
pub struct WorkerSession {
    runtime: WorkerRuntime,
    ticket_id: String,
    fencing: FencingToken,
}
impl WorkerSession {
    pub fn start<V: SignatureVerifier + ?Sized>(
        budget: Arc<Budget>,
        ticket: &ExecutionTicket,
        now_unix_millis: u64,
        minimum_policy_epoch: u64,
        minimum_route_version: u64,
        revocations: &RevocationSnapshot,
        verifier: &V,
    ) -> FaultResult<Self> {
        let runtime = WorkerRuntime::new(budget);
        runtime.start()?;
        runtime.lease(
            ticket,
            now_unix_millis,
            minimum_policy_epoch,
            minimum_route_version,
            revocations,
            verifier,
        )?;
        runtime.run()?;
        Ok(Self {
            runtime,
            ticket_id: ticket.claims.ticket_id.to_string(),
            fencing: ticket.claims.fencing_token,
        })
    }
    #[must_use]
    pub fn state(&self) -> WorkerState {
        self.runtime.state()
    }
    #[must_use]
    pub fn ticket_id(&self) -> &str {
        &self.ticket_id
    }
    #[must_use]
    pub const fn fencing_token(&self) -> FencingToken {
        self.fencing
    }
    pub fn drain(&self, reason: impl Into<String>) -> FaultResult<()> {
        self.runtime.drain(reason)
    }
    pub fn cancel(&self, reason: impl Into<String>) -> FaultResult<()> {
        self.runtime.cancel(reason)
    }
    pub fn commit(&self) -> FaultResult<()> {
        self.runtime.begin_commit(self.fencing)?;
        self.runtime.complete(self.fencing)
    }
    pub fn fail(&self, message: impl Into<String>) -> FaultResult<()> {
        self.runtime.fail(message)
    }
    pub fn require_fence(&self, token: FencingToken) -> FaultResult<()> {
        if token == self.fencing {
            Ok(())
        } else {
            Err(Fault::new(
                mindclade_faults::Code::Conflict,
                "execution session fencing token is stale",
            ))
        }
    }
}
