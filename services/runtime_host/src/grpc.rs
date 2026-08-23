// Copyright © 2026 Mindclade, LLC. All Rights Reserved.
// Mindclade Proprietary and Confidential.
// SPDX-License-Identifier: LicenseRef-Mindclade-Proprietary
//

//! Tonic worker-control service over a permission-restricted Unix socket.

use crate::async_ipc::{bind_secure_listener, remove_owned_socket};
use crate::bootstrap::ControlSession;
use crate::protocol;
use crate::worker_ipc::ModelWorkerConnection;
use crate::{HostAuthority, HostCore};
use mindclade_faults::{Code, Fault, FaultResult, status};
use mindclade_protocols::runtime::v1::worker_control_server::{WorkerControl, WorkerControlServer};
use mindclade_protocols::runtime::v1::{
    CancelCommand, WorkerCommand, WorkerState, WorkerStatus, worker_command,
};
use mindclade_worker_protocol::Digest;
use std::path::PathBuf;
use std::pin::Pin;
use std::sync::Arc;
use std::time::Duration;
use tokio::sync::{mpsc, watch};
use tokio::time::{Instant, sleep_until, timeout};
use tokio_stream::Stream;
use tokio_stream::wrappers::{ReceiverStream, UnixListenerStream};
use tonic::{Request, Response, Status};

const MAX_CONTROL_FRAME_BYTES: usize = 1024 * 1024;
const STATUS_QUEUE_CAPACITY: usize = 32;
const GRPC_DRAIN_TIMEOUT: Duration = Duration::from_secs(30);
const FIRST_COMMAND_TIMEOUT: Duration = Duration::from_secs(30);
const CANCELLATION_GRACE: Duration = Duration::from_secs(5);

#[derive(Clone, Copy, Debug)]
enum TerminalDisposition {
    Cancelled,
    Failed,
}

#[derive(Clone, Copy, Debug)]
struct ActiveExecution {
    model_digest: Digest,
    maximum_output_bytes: u64,
    deadline: Instant,
}

#[derive(Clone, Debug)]
pub struct WorkerControlService {
    host: Arc<HostCore>,
    authority: Arc<HostAuthority>,
}

impl WorkerControlService {
    #[must_use]
    pub fn new(host: Arc<HostCore>, authority: Arc<HostAuthority>) -> Self {
        Self { host, authority }
    }
}

#[tonic::async_trait]
impl WorkerControl for WorkerControlService {
    type ExecuteStream = Pin<Box<dyn Stream<Item = Result<WorkerStatus, Status>> + Send + 'static>>;

    async fn execute(
        &self,
        request: Request<tonic::Streaming<WorkerCommand>>,
    ) -> Result<Response<Self::ExecuteStream>, Status> {
        let mut inbound = request.into_inner();
        let (sender, receiver) = mpsc::channel(STATUS_QUEUE_CAPACITY);
        let host = self.host.clone();
        let authority = self.authority.clone();
        tokio::spawn(async move {
            if let Err(error) = drive_execution(host, authority, &mut inbound, &sender).await {
                let _ = sender.send(Err(fault_status(&error))).await;
            }
        });
        Ok(Response::new(Box::pin(ReceiverStream::new(receiver))))
    }
}

async fn drive_execution(
    host: Arc<HostCore>,
    authority: Arc<HostAuthority>,
    inbound: &mut tonic::Streaming<WorkerCommand>,
    sender: &mpsc::Sender<Result<WorkerStatus, Status>>,
) -> FaultResult<()> {
    let first = timeout(FIRST_COMMAND_TIMEOUT, inbound.message())
        .await
        .map_err(|_| Fault::new(Code::DeadlineExceeded, "worker start command timed out"))?
        .map_err(|error| {
            Fault::new(Code::Unavailable, "worker command stream failed").with_source(error)
        })?
        .ok_or_else(|| Fault::invalid_argument("worker command stream ended before start"))?;
    let (model_digest, maximum_output_bytes, deadline_unix_millis) = execution_context(&first)?;
    let now = unix_millis()?;
    let remaining = deadline_unix_millis
        .checked_sub(now)
        .ok_or_else(|| Fault::new(Code::DeadlineExceeded, "execution ticket deadline expired"))?;
    let execution_deadline = Instant::now()
        .checked_add(Duration::from_millis(remaining))
        .ok_or_else(|| Fault::new(Code::OutOfRange, "execution deadline overflow"))?;
    let socket = host.models().control_socket(&model_digest)?;
    let mut session = ControlSession::new(host.clone(), authority);
    let admitted = session.handle_command(first.clone())?;
    let mut worker = match ModelWorkerConnection::connect(&socket).await {
        Ok(worker) => worker,
        Err(error) => {
            fail_closed(
                &host,
                &model_digest,
                &mut session,
                sender,
                TerminalDisposition::Failed,
                "model-worker connection failed",
            )
            .await?;
            return Err(error);
        }
    };
    if let Err(error) = worker.send(&first).await {
        fail_closed(
            &host,
            &model_digest,
            &mut session,
            sender,
            TerminalDisposition::Failed,
            "model-worker rejected the start command",
        )
        .await?;
        return Err(error);
    }
    if send_status(sender, admitted).await.is_err() {
        return cancel_execution(
            &host,
            &model_digest,
            maximum_output_bytes,
            &mut session,
            &mut worker,
            sender,
            "gateway status stream closed",
        )
        .await;
    }

    drive_active_execution(
        &host,
        ActiveExecution {
            model_digest,
            maximum_output_bytes,
            deadline: execution_deadline,
        },
        inbound,
        &mut session,
        &mut worker,
        sender,
    )
    .await
}

async fn drive_active_execution(
    host: &Arc<HostCore>,
    execution: ActiveExecution,
    inbound: &mut tonic::Streaming<WorkerCommand>,
    session: &mut ControlSession,
    worker: &mut ModelWorkerConnection,
    sender: &mpsc::Sender<Result<WorkerStatus, Status>>,
) -> FaultResult<()> {
    loop {
        tokio::select! {
            () = sender.closed() => {
                return cancel_execution(
                    host,
                    &execution.model_digest,
                    execution.maximum_output_bytes,
                    session,
                    worker,
                    sender,
                    "gateway status stream closed",
                ).await;
            }
            () = sleep_until(execution.deadline) => {
                return cancel_execution(
                    host,
                    &execution.model_digest,
                    execution.maximum_output_bytes,
                    session,
                    worker,
                    sender,
                    "execution deadline exceeded",
                ).await;
            }
            command = inbound.message() => {
                let Ok(command) = command else {
                    return cancel_execution(
                        host,
                        &execution.model_digest,
                        execution.maximum_output_bytes,
                        session,
                        worker,
                        sender,
                        "gateway command stream failed",
                    ).await;
                };
                let Some(command) = command else {
                    return cancel_execution(
                        host,
                        &execution.model_digest,
                        execution.maximum_output_bytes,
                        session,
                        worker,
                        sender,
                        "gateway command stream closed",
                    ).await;
                };
                if forward_gateway_command(
                    host,
                    &execution.model_digest,
                    execution.maximum_output_bytes,
                    session,
                    worker,
                    sender,
                    command,
                ).await? {
                    return Ok(());
                }
            }
            status = worker.receive() => {
                let Ok(status) = status else {
                    return fail_closed(
                        host,
                        &execution.model_digest,
                        session,
                        sender,
                        TerminalDisposition::Failed,
                        "model-worker status stream failed",
                    ).await;
                };
                if forward_worker_status(
                    host,
                    &execution.model_digest,
                    session,
                    worker,
                    sender,
                    status,
                    execution.maximum_output_bytes,
                ).await? {
                    return Ok(());
                }
            }
        }
    }
}

async fn forward_gateway_command(
    host: &Arc<HostCore>,
    model_digest: &Digest,
    maximum_output_bytes: u64,
    session: &mut ControlSession,
    worker: &mut ModelWorkerConnection,
    sender: &mpsc::Sender<Result<WorkerStatus, Status>>,
    command: WorkerCommand,
) -> FaultResult<bool> {
    match command.command.as_ref() {
        Some(worker_command::Command::Cancel(_)) => {
            session.validate_cancel_command(&command, unix_millis()?)?;
            if worker.send(&command).await.is_err() {
                fail_closed(
                    host,
                    model_digest,
                    session,
                    sender,
                    TerminalDisposition::Cancelled,
                    "model-worker cancellation delivery failed",
                )
                .await?;
                return Ok(true);
            }
            await_cancellation(
                host,
                model_digest,
                maximum_output_bytes,
                session,
                worker,
                sender,
                "execution cancellation was not acknowledged",
            )
            .await?;
            Ok(true)
        }
        Some(worker_command::Command::Start(_)) => {
            cancel_execution(
                host,
                model_digest,
                maximum_output_bytes,
                session,
                worker,
                sender,
                "gateway sent a duplicate start command",
            )
            .await?;
            Ok(true)
        }
        Some(_) => {
            let Ok(status) = session.handle_command(command.clone()) else {
                cancel_execution(
                    host,
                    model_digest,
                    maximum_output_bytes,
                    session,
                    worker,
                    sender,
                    "gateway sent an invalid worker command",
                )
                .await?;
                return Ok(true);
            };
            if worker.send(&command).await.is_err() {
                fail_closed(
                    host,
                    model_digest,
                    session,
                    sender,
                    TerminalDisposition::Failed,
                    "model-worker command delivery failed",
                )
                .await?;
                return Ok(true);
            }
            if send_status(sender, status).await.is_err() {
                cancel_execution(
                    host,
                    model_digest,
                    maximum_output_bytes,
                    session,
                    worker,
                    sender,
                    "gateway status stream closed",
                )
                .await?;
                return Ok(true);
            }
            Ok(false)
        }
        None => {
            cancel_execution(
                host,
                model_digest,
                maximum_output_bytes,
                session,
                worker,
                sender,
                "gateway sent an empty worker command",
            )
            .await?;
            Ok(true)
        }
    }
}

async fn forward_worker_status(
    host: &Arc<HostCore>,
    model_digest: &Digest,
    session: &mut ControlSession,
    worker: &mut ModelWorkerConnection,
    sender: &mpsc::Sender<Result<WorkerStatus, Status>>,
    status: Option<WorkerStatus>,
    maximum_output_bytes: u64,
) -> FaultResult<bool> {
    let Some(status) = status else {
        fail_closed(
            host,
            model_digest,
            session,
            sender,
            TerminalDisposition::Failed,
            "model-worker status stream ended before terminal state",
        )
        .await?;
        return Ok(true);
    };
    let Ok(status) = session.observe_worker_status(status, maximum_output_bytes, unix_millis()?)
    else {
        fail_closed(
            host,
            model_digest,
            session,
            sender,
            TerminalDisposition::Failed,
            "model-worker returned an invalid status",
        )
        .await?;
        return Ok(true);
    };
    let terminal = terminal_state(status.state);
    if send_status(sender, status).await.is_err() && !terminal {
        cancel_execution(
            host,
            model_digest,
            maximum_output_bytes,
            session,
            worker,
            sender,
            "gateway status stream closed",
        )
        .await?;
        return Ok(true);
    }
    Ok(terminal)
}

fn execution_context(command: &WorkerCommand) -> FaultResult<(Digest, u64, u64)> {
    let Some(worker_command::Command::Start(start)) = command.command.as_ref() else {
        return Err(Fault::invalid_argument(
            "first worker command must be start",
        ));
    };
    let ticket =
        protocol::execution_ticket(start.ticket.clone().ok_or_else(|| {
            Fault::invalid_argument("start command is missing execution ticket")
        })?)?;
    let model = ticket.claims.model_bundle.ok_or_else(|| {
        Fault::new(
            Code::FailedPrecondition,
            "model execution ticket is missing a model bundle",
        )
    })?;
    Ok((
        model,
        ticket.claims.budget.maximum_output_bytes,
        ticket.claims.deadline_unix_millis,
    ))
}

async fn cancel_execution(
    host: &Arc<HostCore>,
    model_digest: &Digest,
    maximum_output_bytes: u64,
    session: &mut ControlSession,
    worker: &mut ModelWorkerConnection,
    sender: &mpsc::Sender<Result<WorkerStatus, Status>>,
    reason: &str,
) -> FaultResult<()> {
    let now = unix_millis()?;
    let deadline = now
        .checked_add(
            u64::try_from(CANCELLATION_GRACE.as_millis())
                .map_err(|_| Fault::new(Code::OutOfRange, "cancellation grace exceeds u64"))?,
        )
        .ok_or_else(|| Fault::new(Code::OutOfRange, "cancellation deadline overflow"))?;
    let command = WorkerCommand {
        sequence: session.next_command_sequence()?,
        command: Some(worker_command::Command::Cancel(CancelCommand {
            reason: reason.to_owned(),
            deadline_unix_millis: deadline,
        })),
    };
    session.validate_cancel_command(&command, now)?;
    if worker.send(&command).await.is_err() {
        return fail_closed(
            host,
            model_digest,
            session,
            sender,
            TerminalDisposition::Cancelled,
            reason,
        )
        .await;
    }
    await_cancellation(
        host,
        model_digest,
        maximum_output_bytes,
        session,
        worker,
        sender,
        reason,
    )
    .await
}

async fn await_cancellation(
    host: &Arc<HostCore>,
    model_digest: &Digest,
    maximum_output_bytes: u64,
    session: &mut ControlSession,
    worker: &mut ModelWorkerConnection,
    sender: &mpsc::Sender<Result<WorkerStatus, Status>>,
    failure_reason: &str,
) -> FaultResult<()> {
    let outcome = timeout(CANCELLATION_GRACE, async {
        loop {
            let Some(status) = worker.receive().await? else {
                return Ok::<bool, Fault>(false);
            };
            let status =
                session.observe_worker_status(status, maximum_output_bytes, unix_millis()?)?;
            let terminal = terminal_state(status.state);
            send_status(sender, status).await?;
            if terminal {
                return Ok::<bool, Fault>(true);
            }
        }
    })
    .await;
    if matches!(outcome, Ok(Ok(true))) {
        return Ok(());
    }
    fail_closed(
        host,
        model_digest,
        session,
        sender,
        TerminalDisposition::Cancelled,
        failure_reason,
    )
    .await
}

async fn fail_closed(
    host: &Arc<HostCore>,
    model_digest: &Digest,
    session: &mut ControlSession,
    sender: &mpsc::Sender<Result<WorkerStatus, Status>>,
    disposition: TerminalDisposition,
    reason: &str,
) -> FaultResult<()> {
    let models = host.models();
    let digest = *model_digest;
    tokio::task::spawn_blocking(move || models.terminate_worker(&digest))
        .await
        .map_err(|error| {
            Fault::new(Code::Internal, "model-worker termination task failed").with_source(error)
        })??;
    let status = match disposition {
        TerminalDisposition::Cancelled => session.force_cancel(reason, unix_millis()?)?,
        TerminalDisposition::Failed => session.fail_execution(reason, unix_millis()?)?,
    };
    send_status(sender, status).await
}

async fn send_status(
    sender: &mpsc::Sender<Result<WorkerStatus, Status>>,
    status: WorkerStatus,
) -> FaultResult<()> {
    sender.send(Ok(status)).await.map_err(|_| {
        Fault::new(
            Code::Cancelled,
            "worker status consumer closed before terminal state",
        )
    })
}

fn terminal_state(state: i32) -> bool {
    matches!(
        WorkerState::try_from(state).ok(),
        Some(WorkerState::Completed | WorkerState::Cancelled | WorkerState::Failed)
    )
}

fn unix_millis() -> FaultResult<u64> {
    let elapsed = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_err(|error| {
            Fault::new(Code::Internal, "system clock precedes Unix epoch").with_source(error)
        })?;
    u64::try_from(elapsed.as_millis())
        .map_err(|_| Fault::new(Code::OutOfRange, "system clock exceeds u64 milliseconds"))
}

pub async fn serve_unix(
    path: PathBuf,
    service: WorkerControlService,
    mut shutdown: watch::Receiver<bool>,
) -> FaultResult<()> {
    let (listener, identity) = bind_secure_listener(&path)?;
    let incoming = UnixListenerStream::new(listener);
    let mut server_shutdown = shutdown.clone();
    let server = tonic::transport::Server::builder()
        .add_service(
            WorkerControlServer::new(service)
                .max_decoding_message_size(MAX_CONTROL_FRAME_BYTES)
                .max_encoding_message_size(MAX_CONTROL_FRAME_BYTES),
        )
        .serve_with_incoming_shutdown(incoming, async move {
            loop {
                if *server_shutdown.borrow() || server_shutdown.changed().await.is_err() {
                    break;
                }
            }
        });
    tokio::pin!(server);

    loop {
        tokio::select! {
            result = &mut server => {
                result.map_err(|error| {
                    Fault::new(Code::Unavailable, "runtime-host gRPC server failed")
                        .with_source(error)
                })?;
                break;
            }
            changed = shutdown.changed() => {
                if changed.is_err() || *shutdown.borrow() {
                    tokio::time::timeout(GRPC_DRAIN_TIMEOUT, &mut server)
                        .await
                        .map_err(|_| Fault::new(
                            Code::DeadlineExceeded,
                            "runtime-host gRPC drain exceeded its deadline",
                        ))?
                        .map_err(|error| {
                            Fault::new(Code::Unavailable, "runtime-host gRPC server failed")
                                .with_source(error)
                        })?;
                    break;
                }
            }
        }
    }
    remove_owned_socket(&path, identity)
}

/// Renders one of this host's own faults as a gRPC status.
///
/// The table is `mindclade_faults::status`, which mirrors `libs/go/grpcx`. The
/// local `match` this replaces carried the same defect as the one in
/// `services/runtime_gateway`: `NotFound`, `Aborted`, `Unimplemented`,
/// `DataLoss`, and `Unknown` all collapsed into `internal`, which tells the
/// gateway to retry and page for a request that will never succeed.
///
/// `tonic::Code::from(i32)` is total, and the canonical table never yields 0,
/// so a fault can never be rendered as `Ok`.
fn fault_status(error: &Fault) -> Status {
    Status::new(
        tonic::Code::from(status::grpc_code(error.code())),
        error.message(),
    )
}

#[cfg(test)]
mod fault_status_tests {
    use super::{Code, Fault, fault_status, status};

    /// Every fault code renders the canonical gRPC status.
    ///
    /// The codes are restated here rather than read back from
    /// `mindclade_faults::status`. Reading them back would assert only that
    /// this edge calls the shared function, and the defect being closed is
    /// several edges that each called nothing shared: these values are the
    /// client-visible contract and a change to any one of them has to break a
    /// test that names it.
    #[test]
    fn every_fault_code_renders_its_canonical_grpc_status() {
        let expected: &[(Code, tonic::Code)] = &[
            (Code::Cancelled, tonic::Code::Cancelled),
            (Code::Unknown, tonic::Code::Unknown),
            (Code::InvalidArgument, tonic::Code::InvalidArgument),
            (Code::DeadlineExceeded, tonic::Code::DeadlineExceeded),
            (Code::NotFound, tonic::Code::NotFound),
            (Code::AlreadyExists, tonic::Code::AlreadyExists),
            (Code::PermissionDenied, tonic::Code::PermissionDenied),
            (Code::ResourceExhausted, tonic::Code::ResourceExhausted),
            (Code::FailedPrecondition, tonic::Code::FailedPrecondition),
            (Code::Aborted, tonic::Code::Aborted),
            (Code::Conflict, tonic::Code::Aborted),
            (Code::OutOfRange, tonic::Code::OutOfRange),
            (Code::Unimplemented, tonic::Code::Unimplemented),
            (Code::Internal, tonic::Code::Internal),
            (Code::Unavailable, tonic::Code::Unavailable),
            (Code::DataLoss, tonic::Code::DataLoss),
            (Code::Unauthenticated, tonic::Code::Unauthenticated),
        ];
        assert_eq!(
            expected.len(),
            status::ALL.len(),
            "a fault code is missing from this table"
        );
        for &(code, want) in expected {
            let rendered = fault_status(&Fault::new(code, "rendered")).code();
            assert_eq!(rendered, want, "{code} rendered gRPC {rendered:?}");
        }
    }

    /// The regression. `NotFound`, `Aborted`, `Unimplemented`, `DataLoss`, and
    /// `Unknown` all collapsed into `internal` even though gRPC defines an
    /// exact code for each — so a caller could not tell "that does not exist"
    /// or "this method will never exist" from "we broke, retry".
    #[test]
    fn codes_with_an_exact_grpc_counterpart_do_not_collapse_into_internal() {
        for code in [
            Code::NotFound,
            Code::Aborted,
            Code::Unimplemented,
            Code::DataLoss,
            Code::Unknown,
        ] {
            let rendered = fault_status(&Fault::new(code, "rendered")).code();
            assert_ne!(
                rendered,
                tonic::Code::Internal,
                "{code} still collapses into internal"
            );
        }
    }

    /// A fault is a failure by construction, so it must never be rendered as a
    /// success status a client would read as a completed call.
    #[test]
    fn a_fault_is_never_rendered_as_ok() {
        for &code in status::ALL {
            assert_ne!(
                fault_status(&Fault::new(code, "rendered")).code(),
                tonic::Code::Ok,
                "{code} rendered as a success"
            );
        }
    }
}
