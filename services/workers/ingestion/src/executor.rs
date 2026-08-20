// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Ticketed ingestion stage adapter. Scientific normalization remains Python-owned.

use crate::config::IngestionWorkerConfig;
use mindclade_content_digest::Digest;
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_runtime_core::Budget;
use mindclade_worker_protocol::{ExecutionTicket, RevocationSnapshot, SignatureVerifier};
use mindclade_worker_runtime::WorkerRuntime;
use std::sync::Arc;

pub trait IngestionEngine: Send + Sync {
    fn execute(&self, ticket: &ExecutionTicket) -> FaultResult<Vec<Digest>>;
}

#[derive(Debug)]
pub struct IngestionExecutor {
    config: IngestionWorkerConfig,
    budget: Arc<Budget>,
}

impl IngestionExecutor {
    pub fn new(config: IngestionWorkerConfig, budget: Arc<Budget>) -> FaultResult<Self> {
        config.validate()?;
        Ok(Self { config, budget })
    }
    #[allow(clippy::too_many_arguments)]
    pub fn execute<V: SignatureVerifier + ?Sized>(
        &self,
        ticket: &ExecutionTicket,
        now_unix_millis: u64,
        minimum_policy_epoch: u64,
        minimum_route_version: u64,
        revocations: &RevocationSnapshot,
        verifier: &V,
        engine: &dyn IngestionEngine,
    ) -> FaultResult<Vec<Digest>> {
        if ticket.claims.execution_class != "ingestion" {
            return Err(Fault::new(
                Code::FailedPrecondition,
                "execution ticket is not an ingestion stage",
            ));
        }
        let runtime = WorkerRuntime::new(Budget::child(
            self.budget.clone(),
            ticket.claims.ticket_id.to_string(),
            ticket.claims.budget.to_resources(),
        ));
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
        let outputs = match engine.execute(ticket) {
            Ok(outputs) => outputs,
            Err(error) => {
                let _ = runtime.fail(error.to_string());
                return Err(error);
            }
        };
        if outputs.len() > self.config.maximum_outputs {
            let _ = runtime.fail("ingestion output count exceeded configured limit");
            return Err(Fault::new(
                Code::ResourceExhausted,
                "ingestion output count exceeds configured limit",
            ));
        }
        runtime.begin_commit(ticket.claims.fencing_token)?;
        runtime.complete(ticket.claims.fencing_token)?;
        Ok(outputs)
    }
}
