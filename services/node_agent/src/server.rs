// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Framework-independent ticketed stage execution core.

use crate::telemetry::NodeMetrics;
use crate::{NodeAgentConfig, NodeHealth};
use mindclade_faults::{Code, Fault, FaultResult};
use mindclade_runtime_core::{Budget, BudgetHierarchy, CancellationToken, Clock, SystemClock};
use mindclade_worker_protocol::{
    BufferDescriptor, ExecutionTicket, RevocationSnapshot, SignatureVerifier, WorkloadEnvelope,
};
use mindclade_worker_runtime::WorkerRuntime;
use std::collections::BTreeMap;
use std::future::Future;
use std::pin::Pin;
use std::sync::{Arc, Mutex};
use std::time::UNIX_EPOCH;

#[derive(Clone, Debug, Default)]
pub struct StageResult {
    pub outputs: Vec<BufferDescriptor>,
    pub message: String,
}

/// Boxed async stage result used to keep `StageExecutor` object-safe.
pub type StageFuture<'a> = Pin<Box<dyn Future<Output = FaultResult<StageResult>> + Send + 'a>>;

/// Cooperative execution context handed to stage engines.
///
/// Engines must periodically call `ensure_active` around bounded work and must
/// propagate `cancellation` into subprocess/network operations where possible.
/// The node agent owns cancellation and resource policy; the engine owns
/// scientific semantics.
#[derive(Clone)]
pub struct StageContext {
    pub cancellation: CancellationToken,
    pub deadline_unix_millis: u64,
    pub budget: Arc<Budget>,
    clock: Arc<dyn Clock>,
}

impl core::fmt::Debug for StageContext {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("StageContext")
            .field("deadline_unix_millis", &self.deadline_unix_millis)
            .field("cancelled", &self.cancellation.is_cancelled())
            .field("budget", &self.budget)
            .finish_non_exhaustive()
    }
}

impl StageContext {
    #[must_use]
    pub fn new(
        cancellation: CancellationToken,
        deadline_unix_millis: u64,
        budget: Arc<Budget>,
        clock: Arc<dyn Clock>,
    ) -> Self {
        Self {
            cancellation,
            deadline_unix_millis,
            budget,
            clock,
        }
    }

    pub fn now_unix_millis(&self) -> FaultResult<u64> {
        unix_millis(self.clock.as_ref())
    }

    pub fn ensure_active(&self) -> FaultResult<u64> {
        if self.cancellation.is_cancelled() {
            return Err(Fault::new(Code::Cancelled, "stage execution was cancelled"));
        }
        let now = self.now_unix_millis()?;
        if now > self.deadline_unix_millis {
            return Err(Fault::new(
                Code::DeadlineExceeded,
                "stage execution deadline exceeded",
            ));
        }
        Ok(now)
    }
}

/// Asynchronous stage mechanism shared by ingestion, preprocessing,
/// checkpoint-transfer, data-stream, evaluation, and batch executors.
///
/// The boxed-future form keeps the trait object-safe without pulling an async
/// trait macro into the foundational service graph.
pub trait StageExecutor: Send + Sync {
    fn execute<'a>(
        &'a self,
        operation: &'a str,
        inputs: &'a [BufferDescriptor],
        ticket: &'a ExecutionTicket,
        context: &'a StageContext,
    ) -> StageFuture<'a>;
}

pub struct NodeAgentCore {
    config: NodeAgentConfig,
    root_budget: Arc<Budget>,
    health: Arc<NodeHealth>,
    metrics: NodeMetrics,
    active: Mutex<BTreeMap<String, CancellationToken>>,
    clock: Arc<dyn Clock>,
}

impl core::fmt::Debug for NodeAgentCore {
    fn fmt(&self, formatter: &mut core::fmt::Formatter<'_>) -> core::fmt::Result {
        formatter
            .debug_struct("NodeAgentCore")
            .field("config", &self.config)
            .field("health", &self.health.snapshot())
            .field("active_stage_count", &self.active_stage_count())
            .finish_non_exhaustive()
    }
}

impl NodeAgentCore {
    pub fn new(
        config: NodeAgentConfig,
        health: Arc<NodeHealth>,
        metrics: NodeMetrics,
    ) -> FaultResult<Self> {
        Self::with_clock(config, health, metrics, Arc::new(SystemClock))
    }

    pub fn with_clock(
        config: NodeAgentConfig,
        health: Arc<NodeHealth>,
        metrics: NodeMetrics,
        clock: Arc<dyn Clock>,
    ) -> FaultResult<Self> {
        config.validate()?;
        let root_budget = BudgetHierarchy::new(config.node_resources.clone()).root();
        Ok(Self {
            config,
            root_budget,
            health,
            metrics,
            active: Mutex::new(BTreeMap::new()),
            clock,
        })
    }

    /// Execute a canonical workload envelope through the same signed-ticket,
    /// fencing, budget, drain, and commit lifecycle used by direct stages.
    #[allow(clippy::too_many_arguments)]
    pub async fn execute_workload<V: SignatureVerifier + ?Sized, E: StageExecutor + ?Sized>(
        &self,
        workload: &WorkloadEnvelope,
        minimum_policy_epoch: u64,
        minimum_route_version: u64,
        revocations: &RevocationSnapshot,
        verifier: &V,
        executor: &E,
    ) -> FaultResult<StageResult> {
        let now = unix_millis(self.clock.as_ref())?;
        workload.validate(now)?;
        self.execute(
            &workload.ticket,
            &workload.operation,
            &workload.inputs,
            minimum_policy_epoch,
            minimum_route_version,
            revocations,
            verifier,
            executor,
        )
        .await
    }

    #[allow(clippy::too_many_arguments)]
    pub async fn execute<V: SignatureVerifier + ?Sized, E: StageExecutor + ?Sized>(
        &self,
        ticket: &ExecutionTicket,
        operation: &str,
        inputs: &[BufferDescriptor],
        minimum_policy_epoch: u64,
        minimum_route_version: u64,
        revocations: &RevocationSnapshot,
        verifier: &V,
        executor: &E,
    ) -> FaultResult<StageResult> {
        if !self.health.begin() {
            return Err(Fault::new(Code::Unavailable, "node agent is draining"));
        }
        let health_guard = StageHealthGuard {
            health: self.health.as_ref(),
        };
        self.metrics.stage_started();

        if operation.is_empty() || operation.len() > 256 {
            self.metrics.stage_failed();
            return Err(Fault::invalid_argument("stage operation is invalid"));
        }
        if inputs.len() > 4_096 {
            self.metrics.stage_failed();
            return Err(Fault::new(
                Code::ResourceExhausted,
                "stage input descriptor count exceeds limit",
            ));
        }

        let now = unix_millis(self.clock.as_ref())?;
        for descriptor in inputs {
            descriptor.validate(now)?;
        }

        let runtime = WorkerRuntime::new(self.root_budget.clone());
        runtime.start()?;
        runtime.lease(
            ticket,
            now,
            minimum_policy_epoch,
            minimum_route_version,
            revocations,
            verifier,
        )?;
        runtime.run()?;

        let cancellation = runtime.cancellation_token();
        let ticket_id = ticket.claims.ticket_id.to_string();
        {
            let mut active = self
                .active
                .lock()
                .unwrap_or_else(|poisoned| poisoned.into_inner());
            if active
                .insert(ticket_id.clone(), cancellation.clone())
                .is_some()
            {
                let _ = runtime.cancel("duplicate active ticket");
                self.metrics.stage_failed();
                return Err(Fault::new(
                    Code::AlreadyExists,
                    "ticket is already active on node",
                ));
            }
        }
        let active_guard = ActiveTicketGuard {
            active: &self.active,
            ticket_id: ticket_id.clone(),
        };

        // The root reservation protects aggregate node capacity. A separate
        // stage-local budget enforces allocations *within* the ticket without
        // double-counting those allocations against the node reservation.
        let stage_budget = Budget::root(
            format!("stage:{ticket_id}"),
            ticket.claims.budget.to_resources(),
        );
        let context = StageContext::new(
            cancellation,
            ticket.claims.deadline_unix_millis,
            stage_budget,
            self.clock.clone(),
        );
        context.ensure_active()?;

        let result = executor.execute(operation, inputs, ticket, &context).await;
        match result {
            Ok(output) => {
                context.ensure_active()?;
                runtime.begin_commit(ticket.claims.fencing_token)?;
                runtime.complete(ticket.claims.fencing_token)?;
                self.metrics.stage_completed();
                drop(active_guard);
                drop(health_guard);
                Ok(output)
            }
            Err(error) => {
                let _ = runtime.cancel("stage failed");
                self.metrics.stage_failed();
                drop(active_guard);
                drop(health_guard);
                Err(error)
            }
        }
    }

    pub fn build_diagnostics(
        &self,
        node_id: &str,
        cache_bytes: u64,
        telemetry_spool_bytes: u64,
        process_exits: Vec<crate::DiagnosticProcessExit>,
        attributes: BTreeMap<String, String>,
    ) -> FaultResult<crate::NodeDiagnosticsBundle> {
        let active_ticket_ids = self
            .active
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .keys()
            .cloned()
            .collect();
        let bundle = crate::NodeDiagnosticsBundle {
            node_id: node_id.to_owned(),
            runtime_version: env!("CARGO_PKG_VERSION").to_owned(),
            generated_unix_millis: unix_millis(self.clock.as_ref())?,
            budget: self.root_budget.tree_snapshot(),
            active_ticket_ids,
            process_exits,
            cache_bytes,
            telemetry_spool_bytes,
            attributes,
        };
        bundle.validate()?;
        Ok(bundle)
    }

    /// Encode and publish a bounded diagnostics artifact through the canonical
    /// content-addressed artifact transfer.
    pub fn publish_diagnostics(
        &self,
        transfer: &crate::artifact_transfer::ArtifactTransfer,
        bundle: &crate::NodeDiagnosticsBundle,
        maximum_bytes: usize,
    ) -> FaultResult<mindclade_content_digest::Digest> {
        let bytes = bundle.encode(maximum_bytes)?;
        transfer.store(&bytes)
    }

    /// Enter graceful drain mode: reject new stages while allowing admitted
    /// stages to reach their commit point.
    pub fn drain(&self) {
        self.health.drain();
    }

    /// Force cancellation of all admitted stages, used only after the graceful
    /// shutdown budget has expired or an emergency stop is required.
    pub fn cancel_active(&self, reason: &str) {
        let active = self
            .active
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        for token in active.values() {
            token.cancel(reason.to_owned());
        }
    }

    #[must_use]
    pub fn root_budget(&self) -> Arc<Budget> {
        self.root_budget.clone()
    }

    #[must_use]
    pub fn config(&self) -> &NodeAgentConfig {
        &self.config
    }

    #[must_use]
    pub fn health_snapshot(&self) -> crate::NodeHealthSnapshot {
        self.health.snapshot()
    }

    #[must_use]
    pub fn active_stage_count(&self) -> usize {
        self.active
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .len()
    }
}

struct StageHealthGuard<'a> {
    health: &'a NodeHealth,
}

impl Drop for StageHealthGuard<'_> {
    fn drop(&mut self) {
        let _ = self.health.end();
    }
}

struct ActiveTicketGuard<'a> {
    active: &'a Mutex<BTreeMap<String, CancellationToken>>,
    ticket_id: String,
}

impl Drop for ActiveTicketGuard<'_> {
    fn drop(&mut self) {
        self.active
            .lock()
            .unwrap_or_else(|poisoned| poisoned.into_inner())
            .remove(&self.ticket_id);
    }
}

fn unix_millis(clock: &dyn Clock) -> FaultResult<u64> {
    let elapsed = clock.system_now().duration_since(UNIX_EPOCH).map_err(|_| {
        Fault::new(
            Code::FailedPrecondition,
            "node-agent wall clock is before Unix epoch",
        )
    })?;
    u64::try_from(elapsed.as_millis()).map_err(|_| {
        Fault::new(
            Code::OutOfRange,
            "node-agent wall-clock milliseconds exceed u64",
        )
    })
}
